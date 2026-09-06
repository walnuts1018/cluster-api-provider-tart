package extensions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters,verbs=get
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get

const workloadClientTimeout = 10 * time.Second

// updateCordonAnnotationは、providerがupdateのためにcordonしたNodeへ付ける印である。
// update完了後にuncordonしてよいのはこの印があるNodeだけであり、利用者が自分でcordonしたNodeを勝手に戻さない。
const updateCordonAnnotation = "update.tart.walnuts.dev/cordoned"

var errDrainContextUnavailable = errors.New("workload drain context is unavailable")

// drainOutcomeはNodeのdrain結果を表す。evictedAllがfalseの場合、pdbBlockedOnlyが唯一の失敗理由を区別する。
type drainOutcome struct {
	// evictedAllは対象Node上の全evictable Podのevictionに成功した(あるいは対象Podが無かった)ことを示す。
	evictedAll bool
	// pdbBlockedOnlyは全ての失敗がPodDisruptionBudget/availability起因(HTTP 429)であったことを示す。
	// allowDowntime policyの適用対象となるのはこのケースだけである。
	pdbBlockedOnly bool
}

// workloadClientForMachineは、TartControlPlaneのensureKubeconfigSecretが発行する`<cluster-name>-kubeconfig`
// SecretからCAPI Machineが属するworkload clusterへのclient-go clientsetを構築する。
func workloadClientForMachine(ctx context.Context, kubeClient client.Reader, machine *clusterv1.Machine) (kubernetes.Interface, error) {
	if kubeClient == nil || machine == nil || machine.Namespace == "" || machine.Spec.ClusterName == "" {
		return nil, errDrainContextUnavailable
	}
	secret := &corev1.Secret{}
	secretKey := client.ObjectKey{Namespace: machine.Namespace, Name: machine.Spec.ClusterName + "-kubeconfig"}
	if err := kubeClient.Get(ctx, secretKey, secret); err != nil {
		return nil, err
	}
	kubeconfig := secret.Data["value"]
	if len(kubeconfig) == 0 {
		return nil, errDrainContextUnavailable
	}
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("parse workload kubeconfig: %w", err)
	}
	config.Timeout = workloadClientTimeout
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create workload Kubernetes client: %w", err)
	}
	return clientset, nil
}

// findNodeByProviderIDは、workload cluster内でspec.providerIDが一致するNodeを探す。
func findNodeByProviderID(ctx context.Context, clientset kubernetes.Interface, providerID string) (*corev1.Node, error) {
	if providerID == "" {
		return nil, errors.New("provider ID is empty")
	}
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list workload Nodes: %w", err)
	}
	for i := range nodes.Items {
		if nodes.Items[i].Spec.ProviderID == providerID {
			return &nodes.Items[i], nil
		}
	}
	return nil, apierrors.NewNotFound(corev1.Resource("nodes"), providerID)
}

// cordonNodeは対象NodeをUnschedulableにする。すでにcordon済みの場合は何もしない。
func cordonNode(ctx context.Context, clientset kubernetes.Interface, node *corev1.Node) error {
	if node.Spec.Unschedulable {
		return nil
	}
	updated := node.DeepCopy()
	updated.Spec.Unschedulable = true
	if updated.Annotations == nil {
		updated.Annotations = map[string]string{}
	}
	updated.Annotations[updateCordonAnnotation] = "true"
	_, err := clientset.CoreV1().Nodes().Update(ctx, updated, metav1.UpdateOptions{})
	return err
}

// drainNodeは、対象Node上のevictable Podをpolicy/v1 Eviction APIでevictする。
// PodDisruptionBudget違反によるeviction拒否(429 Too Many Requests)は、availability起因の失敗として
// それ以外の失敗(API接続不可等)と区別する。
func drainNode(ctx context.Context, clientset kubernetes.Interface, nodeName string) (drainOutcome, error) {
	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + nodeName})
	if err != nil {
		return drainOutcome{}, fmt.Errorf("list Pods on Node: %w", err)
	}
	sawPDBFailure := false
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !podRequiresEviction(pod) {
			continue
		}
		eviction := &policyv1.Eviction{ObjectMeta: metav1.ObjectMeta{Name: pod.Name, Namespace: pod.Namespace}}
		evictErr := clientset.PolicyV1().Evictions(pod.Namespace).Evict(ctx, eviction)
		switch {
		case evictErr == nil:
			continue
		case apierrors.IsTooManyRequests(evictErr):
			sawPDBFailure = true
		case apierrors.IsNotFound(evictErr):
			continue
		default:
			return drainOutcome{}, fmt.Errorf("evict Pod %s/%s: %w", pod.Namespace, pod.Name, evictErr)
		}
	}
	if sawPDBFailure {
		return drainOutcome{pdbBlockedOnly: true}, nil
	}
	return drainOutcome{evictedAll: true}, nil
}

// podRequiresEvictionは、kubectl drain相当のフィルタリングに従い、Podがevictionの対象かどうかを判定する。
// DaemonSet管理下のPodは全Nodeへ再作成されるためevictionの対象外とし、static/mirror PodはAPI serverから
// evictできないため対象外とする。
func podRequiresEviction(pod *corev1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return false
	}
	if _, mirrored := pod.Annotations[corev1.MirrorPodAnnotationKey]; mirrored {
		return false
	}
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "DaemonSet" {
			return false
		}
	}
	return true
}

// getUpdateTartClusterは、CAPI MachineのClusterNameからCluster、そのInfrastructureRefからTartClusterを辿って取得する。
func getUpdateTartCluster(ctx context.Context, kubeClient client.Reader, machine *clusterv1.Machine) (*infrav1alpha1.TartCluster, error) {
	if kubeClient == nil || machine == nil || machine.Namespace == "" || machine.Spec.ClusterName == "" {
		return nil, errDrainContextUnavailable
	}
	cluster := &clusterv1.Cluster{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: machine.Spec.ClusterName}, cluster); err != nil {
		return nil, err
	}
	ref := cluster.Spec.InfrastructureRef
	if ref.APIGroup != infrav1alpha1.GroupVersion.Group || ref.Kind != "TartCluster" || ref.Name == "" {
		return nil, errors.New("the CAPI Cluster does not reference a TartCluster infrastructure resource")
	}
	tartCluster := &infrav1alpha1.TartCluster{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: ref.Name}, tartCluster); err != nil {
		return nil, err
	}
	return tartCluster, nil
}

// enforceDrainPolicyは、node-disruptiveなTalos restart前に対象NodeのcordonとPDBを尊重したdrainを試みる。
// drainがavailability/PDB/capacity以外の理由で失敗した場合、または当該失敗がallowDowntime policyで
// 許容されていない場合は、Talos Upgradeへ進めずretryを要求する(proceed=false)。対象Nodeが未観測(初回起動
// 直後等でworkload clusterへまだ参加していない)場合は、cordon/drainする対象が存在しないためそのまま進める。
func enforceDrainPolicy(ctx context.Context, kubeClient client.Reader, machine *clusterv1.Machine, providerID string) (proceed bool, retryMessage string) {
	clientset, err := workloadClientForMachine(ctx, kubeClient, machine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// workload kubeconfig Secretがまだ存在しない(control planeが未到達等)場合は、drain対象も存在し得ないためそのまま進める。
			return true, ""
		}
		return false, "The workload cluster Kubernetes client is not available while attempting to drain the Node before the Talos restart."
	}
	drainContext, cancel := context.WithTimeout(ctx, talosUpdateTimeout)
	defer cancel()
	node, err := findNodeByProviderID(drainContext, clientset, providerID)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return true, ""
		}
		return false, "The workload cluster Node could not be observed while attempting to drain it before the Talos restart."
	}
	if err := cordonNode(drainContext, clientset, node); err != nil {
		return false, "The workload cluster Node could not be cordoned before the Talos restart."
	}
	outcome, err := drainNode(drainContext, clientset, node.Name)
	if err != nil {
		return false, "The workload cluster Node drain failed for a reason unrelated to availability or PodDisruptionBudget; waiting before the restart."
	}
	if outcome.evictedAll {
		return true, ""
	}
	// ここに到達するのはPDB/availability起因の失敗だけである。allowDowntime policyでのみ緩和する。
	tartCluster, err := getUpdateTartCluster(ctx, kubeClient, machine)
	if err != nil {
		return false, "The TartCluster update policy could not be observed while a Node drain failure is pending."
	}
	if tartCluster.Spec.UpdatePolicy.DisruptionPolicy == infrav1alpha1.DisruptionPolicyAllowDowntime {
		return true, ""
	}
	return false, "The Node drain was blocked by PodDisruptionBudget or availability constraints; the update is paused until allowDowntime is enabled or the workload becomes evictable."
}

// nodeReadyForMachineは、workload cluster上で対象NodeがReadyであることを観測する。Nodeがまだworkload clusterへ参加していない
// 場合は観測対象が存在しないため、update後の回復確認としては満たされたものとして扱う。
func nodeReadyForMachine(ctx context.Context, kubeClient client.Reader, machine *clusterv1.Machine, providerID string) (bool, string) {
	clientset, err := workloadClientForMachine(ctx, kubeClient, machine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return true, ""
		}
		return false, "The workload cluster Kubernetes client is not available while verifying the Node after the machine configuration update."
	}
	observationContext, cancel := context.WithTimeout(ctx, talosUpdateTimeout)
	defer cancel()
	node, err := findNodeByProviderID(observationContext, clientset, providerID)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return true, ""
		}
		return false, "The workload cluster Node could not be observed while verifying it after the machine configuration update."
	}
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
			if node.Spec.Unschedulable && node.Annotations[updateCordonAnnotation] != "" {
				// providerがupdateのためにcordonしたNodeだけを、updateの完了を確認できた時点でuncordonする。
				if err := uncordonNode(observationContext, clientset, node); err != nil {
					return false, "The workload cluster Node could not be uncordoned after the machine configuration update."
				}
			}
			return true, ""
		}
	}
	return false, "The workload cluster Node is not Ready yet after the machine configuration update."
}

// uncordonNodeは対象NodeのUnschedulableとproviderのcordon印を解除する。
func uncordonNode(ctx context.Context, clientset kubernetes.Interface, node *corev1.Node) error {
	updated := node.DeepCopy()
	updated.Spec.Unschedulable = false
	delete(updated.Annotations, updateCordonAnnotation)
	_, err := clientset.CoreV1().Nodes().Update(ctx, updated, metav1.UpdateOptions{})
	return err
}

// kubernetesVersionConvergedは、観測したKubernetes versionがdesired versionと一致するかを判定する。
// CAPIは先頭にvを付けたversionを扱い、kubeletの報告も同様のため、前後の空白とv接頭辞を無視して比較する。
func kubernetesVersionConverged(observed, desired string) bool {
	normalize := func(value string) string {
		return strings.TrimPrefix(strings.TrimSpace(value), "v")
	}
	desiredVersion := normalize(desired)
	if desiredVersion == "" {
		return false
	}
	return normalize(observed) == desiredVersion
}

// nodeKubernetesVersionConvergedは、対象Nodeのkubeletがdesired Kubernetes versionを報告しているかを確認する。
// Kubernetes version upgradeそのものはTartControlPlaneがcluster単位で実行するため、ここでは収束の観測だけを行う。
func nodeKubernetesVersionConverged(ctx context.Context, kubeClient client.Reader, machine *clusterv1.Machine, providerID, desiredVersion string) (bool, string) {
	if desiredVersion == "" {
		return false, "The desired Kubernetes version of the Machine is empty; the in-place update is waiting for a valid desired state."
	}
	clientset, err := workloadClientForMachine(ctx, kubeClient, machine)
	if err != nil {
		return false, "The workload cluster Kubernetes client is not available while verifying the Kubernetes version of the Node."
	}
	observationContext, cancel := context.WithTimeout(ctx, talosUpdateTimeout)
	defer cancel()
	node, err := findNodeByProviderID(observationContext, clientset, providerID)
	if err != nil {
		return false, "The workload cluster Node could not be observed while verifying its Kubernetes version."
	}
	if !kubernetesVersionConverged(node.Status.NodeInfo.KubeletVersion, desiredVersion) {
		return false, "The workload cluster Node has not converged to the desired Kubernetes version yet; waiting for the cluster-wide Kubernetes upgrade owned by the TartControlPlane."
	}
	return true, ""
}
