package tartcontrolplane

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controller"
	"github.com/walnuts1018/cluster-api-provider-tart/controller/tartmachine"
)

func (r *TartControlPlaneReconciler) reconcileControlPlaneBootstrap(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, cluster *clusterv1.Cluster, machines []clusterv1.Machine) (controlPlaneBootstrapState, error) {
	state := controlPlaneBootstrapState{
		reason:       "MachinesUnavailable",
		message:      "The first control-plane Machine is not running the desired Talos version yet.",
		requeueAfter: 30 * time.Second,
	}
	firstName, err := controlPlaneChildName(cp.Name, 0, "")
	if err != nil {
		return controlPlaneBootstrapState{}, &controlPlaneFailure{reason: controller.ReasonMachineNameInvalid, message: "A deterministic control-plane Machine name is invalid."}
	}
	var firstMachine *clusterv1.Machine
	for index := range machines {
		if machines[index].Name == firstName {
			firstMachine = &machines[index]
			break
		}
	}
	if firstMachine == nil {
		return state, nil
	}
	return r.reconcileFirstControlPlane(ctx, cp, cluster, firstMachine, state)
}

func (r *TartControlPlaneReconciler) reconcileFirstControlPlane(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, cluster *clusterv1.Cluster, machine *clusterv1.Machine, state controlPlaneBootstrapState) (controlPlaneBootstrapState, error) {
	observation, state, ready, err := r.observeFirstControlPlane(ctx, machine, state)
	if err != nil {
		return controlPlaneBootstrapState{}, err
	}
	if !ready {
		return state, nil
	}
	return r.reconcileFirstControlPlaneTalos(ctx, cp, cluster, observation, state)
}

type firstControlPlaneObservation struct {
	endpoint      string
	configuration []byte
}

func (r *TartControlPlaneReconciler) observeFirstControlPlane(ctx context.Context, machine *clusterv1.Machine, state controlPlaneBootstrapState) (firstControlPlaneObservation, controlPlaneBootstrapState, bool, error) {
	if machine == nil || machine.Spec.InfrastructureRef.APIGroup != infrav1alpha1.GroupVersion.Group || machine.Spec.InfrastructureRef.Kind != controller.TartMachineKind || machine.Spec.InfrastructureRef.Name == "" {
		return firstControlPlaneObservation{}, controlPlaneBootstrapState{}, false, &controlPlaneFailure{reason: controller.ReasonMachineSpecMismatch, message: "The first control-plane Machine has an invalid infrastructure reference."}
	}
	var providerMachine infrav1alpha1.TartMachine
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: machine.Spec.InfrastructureRef.Name}, &providerMachine); err != nil {
		if apierrors.IsNotFound(err) {
			return firstControlPlaneObservation{}, state, false, nil
		}
		return firstControlPlaneObservation{}, controlPlaneBootstrapState{}, false, err
	}
	if err := controller.ValidateProviderOwner(&providerMachine, machine, clusterv1.GroupVersion.String(), controller.CAPIMachineKind); err != nil {
		return firstControlPlaneObservation{}, controlPlaneBootstrapState{}, false, err
	}
	ready := meta.FindStatusCondition(providerMachine.Status.Conditions, infrav1alpha1.TartMachineReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		return firstControlPlaneObservation{}, state, false, nil
	}
	if providerMachine.Status.HostRef == nil {
		state.reason = "HostUnavailable"
		state.message = "The first control-plane Machine has no observed TartHost binding yet."
		return firstControlPlaneObservation{}, state, false, nil
	}

	var providerHost infrav1alpha1.TartHost
	if err := r.Get(ctx, client.ObjectKey{Name: providerMachine.Status.HostRef.Name}, &providerHost); err != nil {
		if apierrors.IsNotFound(err) {
			state.reason = "HostUnavailable"
			state.message = "The first control-plane Machine Host is not available yet."
			return firstControlPlaneObservation{}, state, false, nil
		}
		return firstControlPlaneObservation{}, controlPlaneBootstrapState{}, false, err
	}
	if providerHost.Spec.ConsumerRef == nil || providerHost.Spec.ConsumerRef.UID != providerMachine.UID {
		return firstControlPlaneObservation{}, controlPlaneBootstrapState{}, false, &controlPlaneFailure{reason: "HostBindingMismatch", message: "The first control-plane Machine Host binding does not match the provider Machine."}
	}
	endpoint := controller.HostTalosEndpoint(&providerHost)
	if endpoint == "" {
		state.reason = "EndpointUnavailable"
		state.message = "The first control-plane Machine has no reachable Talos endpoint yet."
		return firstControlPlaneObservation{}, state, false, nil
	}
	configuration, err := tartmachine.BootstrapConfiguration(ctx, r.Client, &providerMachine)
	if err != nil {
		if errors.Is(err, tartmachine.ErrBootstrapDataUnavailable) {
			state.reason = "BootstrapDataUnavailable"
			state.message = "The immutable Bootstrap Secret is not available for the first control-plane Machine yet."
			return firstControlPlaneObservation{}, state, false, nil
		}
		return firstControlPlaneObservation{}, controlPlaneBootstrapState{}, false, err
	}
	return firstControlPlaneObservation{endpoint: endpoint, configuration: configuration}, state, true, nil
}

func (r *TartControlPlaneReconciler) reconcileFirstControlPlaneTalos(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, cluster *clusterv1.Cluster, observation firstControlPlaneObservation, state controlPlaneBootstrapState) (controlPlaneBootstrapState, error) {
	// dialAuthenticatedと同様、外部Talos APIへの呼び出しは必ずbounded contextで行う。
	// ここへ生のctx(通常deadlineを持たない)を渡すと、endpointが到達不能な場合に
	// このgRPC呼び出しが無期限にblockし、controller全体の唯一のworkerを永久に停止させる。
	dialContext, dialCancel := context.WithTimeout(ctx, 20*time.Second)
	authenticated, err := talos.DialAuthenticatedFromConfiguration(dialContext, observation.endpoint, observation.configuration)
	dialCancel()
	if err != nil {
		state.reason = "TalosUnavailable"
		state.message = "The authenticated Talos API is not reachable on the first control-plane Machine."
		return state, nil //nolint:nilerr // an unavailable node is a normal reconcile observation.
	}
	etcdStatusContext, etcdStatusCancel := context.WithTimeout(ctx, 10*time.Second)
	etcdStatus, etcdErr := authenticated.EtcdStatus(etcdStatusContext)
	etcdStatusCancel()
	if etcdErr == nil && etcdStatusHealthy(etcdStatus) {
		kubeconfigContext, kubeconfigCancel := context.WithTimeout(ctx, 10*time.Second)
		kubeconfig, kubeconfigErr := authenticated.Kubeconfig(kubeconfigContext)
		kubeconfigCancel()
		if kubeconfigErr != nil {
			if closeErr := authenticated.Close(); closeErr != nil {
				ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
			}
			return controlPlaneBootstrapState{
				etcdReady:    true,
				reason:       controller.ReasonWorkloadAPIUnavailable,
				message:      "Talos etcd is healthy, but the workload kubeconfig is not available yet.",
				requeueAfter: 30 * time.Second,
			}, nil
		}
		if apiErr := workloadAPIReady(ctx, kubeconfig, cluster.Spec.ControlPlaneEndpoint); apiErr != nil {
			if closeErr := authenticated.Close(); closeErr != nil {
				ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
			}
			return controlPlaneBootstrapState{
				etcdReady:    true,
				reason:       controller.ReasonWorkloadAPIUnavailable,
				message:      "Talos etcd is healthy, but the workload Kubernetes API is not ready yet.",
				requeueAfter: 30 * time.Second,
			}, nil
		}
		if kubeconfigErr := r.ensureKubeconfigSecret(ctx, cluster, kubeconfig); kubeconfigErr != nil {
			if closeErr := authenticated.Close(); closeErr != nil {
				ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
			}
			return controlPlaneBootstrapState{
				etcdReady:     true,
				workloadReady: false,
				reason:        "KubeconfigUnavailable",
				message:       "The workload Kubernetes API is ready, but the workload kubeconfig Secret could not be persisted.",
				requeueAfter:  15 * time.Second,
			}, kubeconfigErr
		}
		if closeErr := authenticated.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		return controlPlaneBootstrapState{
			initialized:   true,
			etcdReady:     true,
			workloadReady: true,
			reason:        "EtcdClusterAvailable",
			message:       "The workload Kubernetes API is ready and the first control-plane Machine reports a healthy etcd member and leader.",
		}, nil
	}
	if cp.Status.Initialization.ControlPlaneInitialized != nil && *cp.Status.Initialization.ControlPlaneInitialized {
		// 初回bootstrap後は、etcd観測が失敗してもBootstrap RPCを再実行しない。
		// 再bootstrapは既存clusterのmembershipやdataを壊す可能性があるため、
		// Talosとworkload APIの復旧を観測しながら安全停止する。
		if closeErr := authenticated.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		return controlPlaneBootstrapState{
			reason:       "EtcdClusterUnavailable",
			message:      "The control plane was initialized previously, but a healthy etcd member and leader are not currently observed.",
			requeueAfter: 30 * time.Second,
		}, nil
	}

	bootstrapErr := authenticated.Bootstrap(ctx)
	if closeErr := authenticated.Close(); closeErr != nil {
		ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
	}
	if bootstrapErr != nil && status.Code(bootstrapErr) != codes.AlreadyExists {
		return controlPlaneBootstrapState{
			reason:       "EtcdBootstrapFailed",
			message:      "The first control-plane Machine could not start the Talos etcd bootstrap.",
			requeueAfter: 30 * time.Second,
		}, nil
	}
	return controlPlaneBootstrapState{
		reason:       "Bootstrapping",
		message:      "Talos etcd bootstrap was requested; waiting for a healthy member and leader.",
		requeueAfter: 15 * time.Second,
	}, nil
}

func (r *TartControlPlaneReconciler) ensureKubeconfigSecret(ctx context.Context, cluster *clusterv1.Cluster, kubeconfig []byte) error {
	if cluster == nil || cluster.Namespace == "" || cluster.Name == "" || cluster.UID == "" || len(kubeconfig) == 0 {
		return errors.New("workload kubeconfig identity or data is incomplete")
	}
	expected := &corev1.Secret{
		Namespace:       cluster.Namespace,
		Name:            cluster.Name + "-kubeconfig",
		Labels:          map[string]string{clusterv1.ClusterNameLabel: cluster.Name},
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(cluster, clusterv1.GroupVersion.String(), "Cluster")},
		Type:            clusterv1.ClusterSecretType,
		Data:            map[string][]byte{"value": bytes.Clone(kubeconfig)},
	}

	actual := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: expected.Namespace, Name: expected.Name}, actual)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, expected)
	}
	if err != nil {
		return err
	}
	if actual.Type != clusterv1.ClusterSecretType || actual.Labels[clusterv1.ClusterNameLabel] != cluster.Name || !controller.HasControllerOwner(actual, cluster, clusterv1.GroupVersion.String(), "Cluster") {
		return errors.New("existing workload kubeconfig Secret does not satisfy the CAPI contract")
	}
	if bytes.Equal(actual.Data["value"], kubeconfig) && len(actual.Data) == 1 {
		return nil
	}
	original := actual.DeepCopy()
	actual.Data = map[string][]byte{"value": bytes.Clone(kubeconfig)}
	actual.Type = clusterv1.ClusterSecretType
	return r.Patch(ctx, actual, client.MergeFrom(original))
}

func workloadAPIReady(ctx context.Context, kubeconfig []byte, endpoint clusterv1.APIEndpoint) error {
	if len(kubeconfig) == 0 {
		return errors.New("workload kubeconfig is empty")
	}
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return fmt.Errorf("parse workload kubeconfig: %w", err)
	}
	if endpoint.IsValid() {
		config.Host = "https://" + endpoint.String()
	}
	config.Timeout = 10 * time.Second
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create workload Kubernetes client: %w", err)
	}
	if err := clientset.Discovery().RESTClient().Get().AbsPath("/readyz").Do(ctx).Error(); err != nil {
		return fmt.Errorf("check workload Kubernetes API readiness: %w", err)
	}
	return nil
}
