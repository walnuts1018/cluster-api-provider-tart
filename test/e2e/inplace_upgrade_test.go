//go:build e2e

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/test/e2e/framework"
)

// upgradeTargetTalosVersion/upgradeTargetSchematicID/upgradeTargetKubernetesVersionは、
// FreshProvisionが構築したclusterに対してin-place upgradeを要求するdesired stateである。
// upgradeTargetTalosVersionはe2eTalosVersionと意図的に同じ値を使う: Talos Image Factoryは
// リリース済みのversionにしかinstaller imageを提供しないため、実在しないversionを指定すると
// Upgrade RPCが正当な"image not found"で失敗し、provider側の欠陥ではなくtest fixtureの欠陥に
// なってしまう。かわりにupgradeTargetSchematicIDでsystem extension setを変更し、
// SchematicMismatch検知経由のin-place upgrade経路(applyTalosUpgrade→PerformImageUpgrade→
// 実際のimage pull+Upgrade RPC)を実在するimageで検証する。
const (
	upgradeTargetTalosVersion = e2eTalosVersion
	// upgradeTargetSchematicIDは、qemu-guest-agent extensionを追加したcustomizationに対応する
	// Talos Image Factoryのschematic identifierである(POST https://factory.talos.dev/schematics
	// で払い出し済みのものを固定値として使用する)。
	upgradeTargetSchematicID       = "ce4c980550dd2ab1b17bbf2b08801c7eb59418eafe8f279833297925d67c7515"
	upgradeTargetKubernetesVersion = "v1.34.1"

	upgradeDataPayload = "tart-e2e-in-place-upgrade-marker"
)

// upgradeIdentityRecordは、upgrade前後でMachine replacementが発生していないことを検証するために
// 記録するidentityの集合である。program counterではなく、単純な「upgrade前後で値が変わらない」
// ことのassertに使う観測値である。
type upgradeIdentityRecord struct {
	machineUID     types.UID
	tartMachineUID types.UID
	tartHostName   string
	nodeUID        types.UID
	dataChecksum   string
}

var recordedIdentity upgradeIdentityRecord

// inPlaceUpgradeSpecsは、InPlaceUpgrade specをginkgoのspec treeへ登録する。suite_test.goの
// 共通Ordered containerからfreshProvisionSpecsの後に呼び出される想定である(FreshProvisionが
// 構築した同一clusterをそのまま利用するため)。
func inPlaceUpgradeSpecs() {
	Describe("InPlaceUpgrade", Ordered, func() {
		BeforeAll(func() {
			By("recording pre-upgrade identity (Machine/TartMachine/TartHost binding/Node UID) and writing a checksummed marker")
			recordedIdentity = recordCurrentIdentity(ctx)
		})

		It("upgrades Talos OS in place without replacing the Machine or losing disk-backed data", func() {
			// TODO: TartMachineTemplate.spec.template.spec.imageの更新だけで既存TartMachineへ
			// desired imageが伝播するのか、CAPI Update Extension経由で個々のTartMachine.spec.image
			// への直接patchが必要なのかは、controller/tartmachine, controller/tartcontrolplane
			// reconcilerの実装挙動を実CIで確認して確定する必要がある。骨格実装では両方を明示的に
			// 更新することで、どちらの経路でもTalosUpToDate=Trueへ収束することを期待する。
			By("bumping TartMachineTemplate's desired Talos image and waiting for TalosUpToDate")
			Expect(updateMachineTemplateImage(ctx, upgradeTargetTalosVersion, upgradeTargetSchematicID)).To(Succeed())

			var machine clusterv1.Machine
			Expect(findMachineForCluster(ctx, e2eNamespace, e2eClusterName, &machine)).To(Succeed())
			updatedGeneration, err := updateTartMachineImage(ctx, machine.Spec.InfrastructureRef.Name, upgradeTargetTalosVersion, upgradeTargetSchematicID)
			Expect(err).NotTo(HaveOccurred())

			// TartMachineはimage更新前から既にTalosUpToDate=Trueだったため、単純にcondition.Status
			// だけを見るとreconcilerがまだ新しいgenerationを処理していない古い観測値へ即座に
			// マッチしてしまう。condition.ObservedGenerationが更新後のgeneration以上になるまでは
			// 「まだ報告されていない」ものとして扱うことで、実際に再reconcileされたことを保証する。
			controllerHealthy := framework.NewControllerPodsHealthyCheck(k8sClient, tartSystemNamespace)
			framework.WaitForConditionUntilTerminal(ctx, conditionsAtGeneration(tartMachineConditions(e2eNamespace, machine.Spec.InfrastructureRef.Name), infrav1alpha1.TartMachineTalosUpToDateCondition, updatedGeneration), infrav1alpha1.TartMachineTalosUpToDateCondition, metav1.ConditionTrue, clusterProvisioningTerminalReasons, 20*time.Minute, controllerHealthy)
			waitForTartMachineTalosReady(ctx, machine.Spec.InfrastructureRef.Name, upgradeTargetTalosVersion, upgradeTargetSchematicID)

			assertIdentityUnchanged(ctx, recordedIdentity)
		})

		It("upgrades Kubernetes cluster-wide without replacing any Machine", func() {
			Expect(updateControlPlaneKubernetesVersion(ctx, upgradeTargetKubernetesVersion)).To(Succeed())

			controllerHealthy := framework.NewControllerPodsHealthyCheck(k8sClient, tartSystemNamespace)
			upgradeWaitStart := time.Now()
			Eventually(func(g Gomega) {
				g.Expect(controllerHealthy(ctx)).To(Succeed())
				var updated controlplanev1alpha1.TartControlPlane
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: e2eNamespace, Name: e2eClusterName}, &updated)).To(Succeed())
				GinkgoWriter.Printf("[%s elapsed] TartControlPlane %s/%s: KubernetesUpgrade.ObservedVersion=%q (want %q)\n",
					time.Since(upgradeWaitStart).Round(time.Second), e2eNamespace, e2eClusterName, updated.Status.KubernetesUpgrade.ObservedVersion, upgradeTargetKubernetesVersion)
				// ObservedVersionは検出したcluster Kubernetes versionをそのまま格納しており、
				// spec側のような先頭"v"を持たない場合があるため、比較前に両辺から取り除く。
				g.Expect(strings.TrimPrefix(updated.Status.KubernetesUpgrade.ObservedVersion, "v")).To(Equal(strings.TrimPrefix(upgradeTargetKubernetesVersion, "v")))
			}).WithContext(ctx).WithTimeout(20 * time.Minute).WithPolling(framework.DefaultPollInterval).Should(Succeed())

			framework.WaitForConditionUntilTerminal(ctx, tartControlPlaneConditions(e2eNamespace, e2eClusterName), controlplanev1alpha1.TartControlPlaneAvailableCondition, metav1.ConditionTrue, clusterProvisioningTerminalReasons, 20*time.Minute, controllerHealthy)

			var machine clusterv1.Machine
			Expect(findMachineForCluster(ctx, e2eNamespace, e2eClusterName, &machine)).To(Succeed())
			waitForTartMachineTalosReady(ctx, machine.Spec.InfrastructureRef.Name, upgradeTargetTalosVersion, upgradeTargetSchematicID)
			assertIdentityUnchanged(ctx, recordedIdentity)
		})
	})
}

// recordCurrentIdentityはMachine/TartMachine/TartHost binding/Node UIDと、Talos UserVolume上のmarkerを記録する。
func recordCurrentIdentity(ctx context.Context) upgradeIdentityRecord {
	var machine clusterv1.Machine
	Expect(findMachineForCluster(ctx, e2eNamespace, e2eClusterName, &machine)).To(Succeed())

	var tartMachine infrav1alpha1.TartMachine
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: e2eNamespace, Name: machine.Spec.InfrastructureRef.Name}, &tartMachine)).To(Succeed())
	Expect(tartMachine.Status.HostRef).NotTo(BeNil())

	Expect(machine.Status.NodeRef.IsDefined()).To(BeTrue(), "CAPI Machine must reference a Node before writing the UserVolume marker")
	node, err := getWorkloadNode(ctx, machine.Status.NodeRef.Name)
	Expect(err).NotTo(HaveOccurred())

	sum := sha256.Sum256([]byte(upgradeDataPayload))
	checksum := hex.EncodeToString(sum[:])
	Expect(writeUserVolumeMarker(ctx, node.Name)).To(Succeed())

	return upgradeIdentityRecord{
		machineUID:     machine.UID,
		tartMachineUID: tartMachine.UID,
		tartHostName:   tartMachine.Status.HostRef.Name,
		nodeUID:        node.UID,
		dataChecksum:   checksum,
	}
}

// assertIdentityUnchangedは、upgrade前後でMachine/TartMachine/TartHost binding/Nodeのidentityが
// 不変であり、UserVolume上のmarkerが読み取れることを検証する。
func assertIdentityUnchanged(ctx context.Context, before upgradeIdentityRecord) {
	var machine clusterv1.Machine
	Expect(findMachineForCluster(ctx, e2eNamespace, e2eClusterName, &machine)).To(Succeed())
	Expect(machine.UID).To(Equal(before.machineUID), "Machine UID must not change across in-place upgrade")

	var tartMachine infrav1alpha1.TartMachine
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: e2eNamespace, Name: machine.Spec.InfrastructureRef.Name}, &tartMachine)).To(Succeed())
	Expect(tartMachine.UID).To(Equal(before.tartMachineUID), "TartMachine UID must not change across in-place upgrade")
	Expect(tartMachine.Status.HostRef).NotTo(BeNil())
	Expect(tartMachine.Status.HostRef.Name).To(Equal(before.tartHostName), "TartHost binding must not change across in-place upgrade")

	Expect(machine.Status.NodeRef.IsDefined()).To(BeTrue())
	node := waitForWorkloadNode(ctx, machine.Status.NodeRef.Name)
	Expect(node.UID).To(Equal(before.nodeUID), "Node UID must not change across in-place upgrade")
	Expect(verifyUserVolumeMarker(ctx, node.Name, before.dataChecksum)).To(Succeed())
}

func findMachineForCluster(ctx context.Context, namespace, clusterName string, out *clusterv1.Machine) error {
	var machines clusterv1.MachineList
	if err := k8sClient.List(ctx, &machines, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("list Machines: %w", err)
	}
	var found *clusterv1.Machine
	for i := range machines.Items {
		machine := &machines.Items[i]
		if machine.Spec.ClusterName != clusterName || !machine.DeletionTimestamp.IsZero() {
			continue
		}
		if found != nil {
			return fmt.Errorf("multiple active Machines found for cluster %s/%s", namespace, clusterName)
		}
		found = machine
	}
	if found != nil {
		*out = *found
		return nil
	}
	return fmt.Errorf("no Machine found for cluster %s/%s", namespace, clusterName)
}

func updateMachineTemplateImage(ctx context.Context, version, schematicID string) error {
	return updateOnConflict(ctx, func() error {
		var machineTemplate infrav1alpha1.TartMachineTemplate
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: e2eNamespace, Name: e2eClusterName + "-cp"}, &machineTemplate); err != nil {
			return err
		}
		machineTemplate.Spec.Template.Spec.Image.Version = version
		machineTemplate.Spec.Template.Spec.Image.SchematicID = schematicID
		return k8sClient.Update(ctx, &machineTemplate)
	})
}

// updateTartMachineImageはTartMachine.Spec.Image.Versionを更新し、成功したUpdate呼び出しが
// 返した更新後のGenerationを返す。呼び出し側はこのGenerationを使って、更新前から既に
// True/満たされていたConditionの古い観測値と、再reconcile後の新しい観測値を区別できる。
func updateTartMachineImage(ctx context.Context, name, version, schematicID string) (int64, error) {
	var generation int64
	err := updateOnConflict(ctx, func() error {
		var machine infrav1alpha1.TartMachine
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: e2eNamespace, Name: name}, &machine); err != nil {
			return err
		}
		machine.Spec.Image.Version = version
		machine.Spec.Image.SchematicID = schematicID
		if err := k8sClient.Update(ctx, &machine); err != nil {
			return err
		}
		generation = machine.Generation
		return nil
	})
	return generation, err
}

// conditionsAtGenerationは、指定したconditionTypeの観測値がminGenerationより古い
// (ObservedGeneration < minGeneration)場合、そのconditionを「まだ報告されていない」ものとして
// 除外するConditionGetterラッパーである。更新前から既にexpected statusを満たしていた
// conditionが、更新後の再reconcileを経ずに古い観測値のままEventuallyを通過することを防ぐ。
func conditionsAtGeneration(get framework.ConditionGetter, conditionType string, minGeneration int64) framework.ConditionGetter {
	return func(ctx context.Context) ([]metav1.Condition, error) {
		conditions, err := get(ctx)
		if err != nil {
			return nil, err
		}
		filtered := make([]metav1.Condition, 0, len(conditions))
		for _, condition := range conditions {
			if condition.Type == conditionType && condition.ObservedGeneration < minGeneration {
				continue
			}
			filtered = append(filtered, condition)
		}
		return filtered, nil
	}
}

func updateControlPlaneKubernetesVersion(ctx context.Context, version string) error {
	return updateOnConflict(ctx, func() error {
		var controlPlane controlplanev1alpha1.TartControlPlane
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: e2eNamespace, Name: e2eClusterName}, &controlPlane); err != nil {
			return err
		}
		controlPlane.Spec.Version = version
		return k8sClient.Update(ctx, &controlPlane)
	})
}

func updateOnConflict(ctx context.Context, update func() error) error {
	var lastConflict error
	err := wait.ExponentialBackoffWithContext(ctx, retry.DefaultRetry, func(context.Context) (bool, error) {
		err := update()
		if err == nil {
			return true, nil
		}
		if apierrors.IsConflict(err) {
			lastConflict = err
			return false, nil
		}
		return false, err
	})
	if errors.Is(err, wait.ErrWaitTimeout) && lastConflict != nil {
		return lastConflict
	}
	return err
}

func newWorkloadClient(ctx context.Context) (kubernetes.Interface, error) {
	var cluster clusterv1.Cluster
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: e2eNamespace, Name: e2eClusterName}, &cluster); err != nil {
		return nil, fmt.Errorf("get workload Cluster: %w", err)
	}
	if !cluster.Spec.ControlPlaneEndpoint.IsValid() {
		return nil, errors.New("workload Cluster has no valid control-plane endpoint")
	}

	var secret corev1.Secret
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: e2eNamespace, Name: e2eClusterName + "-kubeconfig"}, &secret); err != nil {
		return nil, fmt.Errorf("get workload kubeconfig Secret: %w", err)
	}
	kubeconfig, ok := secret.Data["value"]
	if secret.Type != clusterv1.ClusterSecretType || !ok || len(kubeconfig) == 0 {
		return nil, errors.New("workload kubeconfig Secret does not satisfy the CAPI contract")
	}

	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("parse workload kubeconfig: %w", err)
	}
	config.Host = "https://" + cluster.Spec.ControlPlaneEndpoint.String()
	config.Timeout = 30 * time.Second
	workload, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create workload Kubernetes client: %w", err)
	}
	return workload, nil
}

// getWorkloadNodeは、CAPI Machine.Status.NodeRefが指すNodeを取得する。NodeはTalosが構築する
// workload clusterのapiserverだけが保持しており、management (kind) clusterのk8sClientでは
// 参照できないため、newWorkloadClientが生成するworkload cluster向けclient-go clientを使う。
func getWorkloadNode(ctx context.Context, name string) (corev1.Node, error) {
	workload, err := newWorkloadClient(ctx)
	if err != nil {
		return corev1.Node{}, err
	}
	node, err := workload.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return corev1.Node{}, fmt.Errorf("get workload Node %q: %w", name, err)
	}
	return *node, nil
}

// waitForWorkloadNodeは、in-place upgradeでTalosが再起動した直後、kube-apiserverの静的Podが
// まだ起動しきっていない一時的な接続不可期間を許容してNodeを取得する。TalosUpToDate/Ready
// conditionはTalos APIの到達性だけを見て収束するため、それらのconditionがTrueになった直後でも
// kube-apiserverはまだ受け付け可能になっていないことがある。
func waitForWorkloadNode(ctx context.Context, name string) corev1.Node {
	var node corev1.Node
	Eventually(func(g Gomega) {
		observed, err := getWorkloadNode(ctx, name)
		g.Expect(err).NotTo(HaveOccurred())
		node = observed
	}).WithContext(ctx).WithTimeout(3 * time.Minute).WithPolling(framework.DefaultPollInterval).Should(Succeed())
	return node
}

func writeUserVolumeMarker(ctx context.Context, nodeName string) error {
	command := fmt.Sprintf("set -eu; printf '%%s' '%s' > /data/marker; test \"$(cat /data/marker)\" = '%s'; sha256sum /data/marker", upgradeDataPayload, upgradeDataPayload)
	output, err := runUserVolumeCommand(ctx, nodeName, command)
	if err != nil {
		return err
	}
	return verifyChecksumOutput(output, dataChecksum())
}

func verifyUserVolumeMarker(ctx context.Context, nodeName, expectedChecksum string) error {
	command := fmt.Sprintf("set -eu; test \"$(cat /data/marker)\" = '%s'; sha256sum /data/marker", upgradeDataPayload)
	output, err := runUserVolumeCommand(ctx, nodeName, command)
	if err != nil {
		return err
	}
	return verifyChecksumOutput(output, expectedChecksum)
}

func dataChecksum() string {
	sum := sha256.Sum256([]byte(upgradeDataPayload))
	return hex.EncodeToString(sum[:])
}

func verifyChecksumOutput(output, expected string) error {
	fields := strings.Fields(output)
	if len(fields) == 0 || fields[0] != expected {
		return fmt.Errorf("UserVolume marker checksum %q does not match expected checksum %q", strings.TrimSpace(output), expected)
	}
	return nil
}

func runUserVolumeCommand(ctx context.Context, nodeName, command string) (output string, returnErr error) {
	workload, err := newWorkloadClient(ctx)
	if err != nil {
		return "", err
	}
	hostPathType := corev1.HostPathDirectory
	privileged := true
	pod := &corev1.Pod{
		GenerateName: "tart-e2e-volume-check-",
		Namespace:    "kube-system",
		Spec: corev1.PodSpec{
			NodeName:      nodeName,
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:    "volume-check",
				Image:   "busybox:1.36.1",
				Command: []string{"sh", "-c", command},
				SecurityContext: &corev1.SecurityContext{
					Privileged: &privileged,
				},
				VolumeMounts: []corev1.VolumeMount{{Name: "user-volume", MountPath: "/data"}},
			}},
			Volumes: []corev1.Volume{{
				Name: "user-volume",
				VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{
					Path: e2eDataVolumeHostPath,
					Type: &hostPathType,
				}},
			}},
		},
	}
	created, err := workload.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("create UserVolume check Pod: %w", err)
	}

	completed, runErr := waitForPodCompletion(ctx, workload, created)
	if runErr == nil {
		output, runErr = readPodLogs(ctx, workload, completed)
	}
	cleanupErr := deletePodAndWait(ctx, workload, created)
	if runErr != nil && cleanupErr != nil {
		return "", errors.Join(runErr, cleanupErr)
	}
	if runErr != nil {
		return "", runErr
	}
	if cleanupErr != nil {
		return "", cleanupErr
	}
	return output, nil
}

func waitForPodCompletion(ctx context.Context, workload kubernetes.Interface, pod *corev1.Pod) (corev1.Pod, error) {
	var completed corev1.Pod
	err := wait.PollUntilContextTimeout(ctx, framework.DefaultPollInterval, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		current, err := workload.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if current.DeletionTimestamp != nil {
			return false, fmt.Errorf("UserVolume check Pod %s/%s is terminating before completion", current.Namespace, current.Name)
		}
		switch current.Status.Phase {
		case corev1.PodSucceeded:
			completed = *current
			return true, nil
		case corev1.PodFailed:
			return false, fmt.Errorf("UserVolume check Pod %s/%s failed", current.Namespace, current.Name)
		default:
			return false, nil
		}
	})
	if err != nil {
		return corev1.Pod{}, fmt.Errorf("wait for UserVolume check Pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	return completed, nil
}

func readPodLogs(ctx context.Context, workload kubernetes.Interface, pod corev1.Pod) (string, error) {
	stream, err := workload.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: "volume-check"}).Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("open UserVolume check Pod logs: %w", err)
	}
	data, readErr := io.ReadAll(stream)
	closeErr := stream.Close()
	if readErr != nil {
		return "", fmt.Errorf("read UserVolume check Pod logs: %w", readErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close UserVolume check Pod logs: %w", closeErr)
	}
	return string(data), nil
}

func deletePodAndWait(ctx context.Context, workload kubernetes.Interface, pod *corev1.Pod) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := workload.CoreV1().Pods(pod.Namespace).Delete(cleanupCtx, pod.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete UserVolume check Pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	err := wait.PollUntilContextTimeout(cleanupCtx, framework.DefaultPollInterval, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		_, err := workload.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("wait for UserVolume check Pod %s/%s deletion: %w", pod.Namespace, pod.Name, err)
	}
	return nil
}

func tartMachineConditions(namespace, name string) framework.ConditionGetter {
	return func(ctx context.Context) ([]metav1.Condition, error) {
		var tartMachine infrav1alpha1.TartMachine
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &tartMachine); err != nil {
			return nil, fmt.Errorf("get TartMachine %s/%s: %w", namespace, name, err)
		}
		return tartMachine.Status.Conditions, nil
	}
}
