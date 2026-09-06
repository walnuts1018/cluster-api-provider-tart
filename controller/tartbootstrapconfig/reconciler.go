package tartbootstrapconfig

import (
	"bytes"
	"context"
	"errors"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos/certbuilder"
	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controller"
	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
	domaincontrolplane "github.com/walnuts1018/cluster-api-provider-tart/domain/controlplane"
	"github.com/walnuts1018/cluster-api-provider-tart/usecase/bootstrap"
	hostpolicy "github.com/walnuts1018/cluster-api-provider-tart/usecase/host"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TartBootstrapConfigReconcilerはTartBootstrapConfig objectをreconcileする。
type TartBootstrapConfigReconciler struct {
	client.Client
	Renderer bootstrap.ConfigRenderer
}

// NewTartBootstrapConfigReconcilerはclientとConfigRendererを設定したTartBootstrapConfigReconcilerを構築する。
func NewTartBootstrapConfigReconciler(c client.Client, renderer bootstrap.ConfigRenderer) *TartBootstrapConfigReconciler {
	return &TartBootstrapConfigReconciler{Client: c, Renderer: renderer}
}

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch

func (r *TartBootstrapConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var config bootstrapv1alpha1.TartBootstrapConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if controller.IsPaused(&config) {
		return ctrl.Result{}, nil
	}

	var input *corev1.Secret
	if config.Spec.ConfigPatchesSecretRef != nil {
		if config.Spec.ConfigPatchesSecretRef.Name == "" {
			return r.report(ctx, &config, "ConfigurationSecretUnavailable", "The configuration Secret reference has no name.")
		}
		input = &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: config.Spec.ConfigPatchesSecretRef.Name}, input); err != nil {
			if apierrors.IsNotFound(err) {
				return r.report(ctx, &config, "ConfigurationSecretUnavailable", "The referenced immutable configuration Secret is not available.")
			}
			return ctrl.Result{}, err
		}
	}
	completeConfiguration, err := r.configuration(ctx, &config, input)
	if err != nil {
		if retryable := errors.Is(err, controller.ErrCAPIMachineUnavailable) || errors.Is(err, errBootstrapContextUnavailable) || errors.Is(err, domainbootstrap.ErrInstallDiskUnavailable); retryable {
			result, reportErr := r.report(ctx, &config, "ClusterContextUnavailable", "The CAPI Cluster and active Talos secret bundle are not available for configuration generation yet.")
			if reportErr != nil {
				return ctrl.Result{}, reportErr
			}
			result.RequeueAfter = 15 * time.Second
			return result, nil
		}
		reason, message := classifyConfigurationError(err)
		return r.report(ctx, &config, reason, message)
	}
	digest, err := r.Renderer.Digest(completeConfiguration)
	if err != nil {
		return r.report(ctx, &config, "ConfigurationInvalid", "The rendered Talos machine configuration is not valid for boot.")
	}

	clusterName := config.Labels[bootstrap.ClusterNameLabel]
	if clusterName == "" {
		return r.report(ctx, &config, "ClusterNameUnavailable", "The cluster.x-k8s.io/cluster-name label is required to create the Bootstrap Secret.")
	}
	owner := metav1.OwnerReference{
		APIVersion: bootstrapv1alpha1.GroupVersion.String(),
		Kind:       controller.TartBootstrapConfigKind,
		Name:       config.Name,
		UID:        config.UID,
	}
	expected, err := bootstrap.BuildSecret(config.Namespace, config.Name, clusterName, owner, completeConfiguration)
	if err != nil {
		return r.report(ctx, &config, "BootstrapSecretInvalid", "The Bootstrap Secret owner or metadata cannot satisfy the CAPI contract.")
	}

	actual := &corev1.Secret{}
	err = r.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: config.Name}, actual)
	switch {
	case apierrors.IsNotFound(err):
		if err := r.Create(ctx, expected); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return ctrl.Result{RequeueAfter: time.Nanosecond}, nil
			}
			return ctrl.Result{}, err
		}
		actual = expected
	case err != nil:
		return ctrl.Result{}, err
	default:
		if !bootstrap.IsContractSecret(actual, clusterName, config.UID) {
			return r.report(ctx, &config, "BootstrapSecretInvalid", "The existing Bootstrap Secret does not satisfy the CAPI contract.")
		}
		if !bytes.Equal(actual.Data[bootstrap.BootstrapSecretKey], completeConfiguration) {
			// Bootstrap Secretはimmutableなためdataを書き換えられない。update policyが変更を許す場合だけ、
			// 同じ名前で作り直してdesired configurationをUpdate Extensionから観測できるようにする。
			if err := r.Delete(ctx, actual); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			if err := r.Create(ctx, expected); err != nil {
				if apierrors.IsAlreadyExists(err) {
					return ctrl.Result{RequeueAfter: time.Nanosecond}, nil
				}
				return ctrl.Result{}, err
			}
			actual = expected
		}
	}

	// Bootstrap Secretはimmutableであり、同じdesired stateの再reconcileでは既存Secretを観測してStatusだけを更新する。
	// desired configurationが変わった場合の扱いはconfiguration update policyが決める。
	original := config.DeepCopy()
	config.Status.Initialization.DataSecretCreated = new(true)
	config.Status.DataSecretName = actual.Name
	config.Status.ConfigurationDigest = digest
	controller.SetCondition(&config.Status.Conditions, bootstrapv1alpha1.TartBootstrapConfigReadyCondition, metav1.ConditionTrue, "DataSecretCreated", "The immutable Bootstrap Secret is available.", config.Generation)
	config.Status.ObservedGeneration = config.Generation
	if err := r.Status().Patch(ctx, &config, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

var errBootstrapContextUnavailable = errors.New("bootstrap cluster context is unavailable")
var errBootstrapIdentityConflict = errors.New("bootstrap Host identity conflict")

func (r *TartBootstrapConfigReconciler) configuration(ctx context.Context, config *bootstrapv1alpha1.TartBootstrapConfig, input *corev1.Secret) ([]byte, error) {
	if input == nil {
		return r.configurationFromPatches(ctx, config, nil)
	}
	if err := bootstrap.ValidateConfigSecret(input); err != nil {
		return nil, err
	}
	if len(input.Data[bootstrap.ConfigurationInputKey]) > 0 || len(input.Data[bootstrap.ConfigurationPatchesKey]) == 0 {
		return r.configurationFromValue(ctx, config, input)
	}
	return r.configurationFromPatches(ctx, config, input.Data[bootstrap.ConfigurationPatchesKey])
}

func (r *TartBootstrapConfigReconciler) configurationFromValue(ctx context.Context, config *bootstrapv1alpha1.TartBootstrapConfig, input *corev1.Secret) ([]byte, error) {
	configurationContext, err := r.machineConfigurationContext(ctx, config)
	if err != nil {
		return nil, err
	}
	return bootstrap.RenderFromCompleteValue(r.Renderer, input, configurationContext)
}

func (r *TartBootstrapConfigReconciler) configurationFromPatches(ctx context.Context, config *bootstrapv1alpha1.TartBootstrapConfig, patches []byte) ([]byte, error) {
	configurationContext, err := r.machineConfigurationContext(ctx, config)
	if err != nil {
		return nil, err
	}
	return bootstrap.RenderFromPatches(r.Renderer, configurationContext, patches)
}

func (r *TartBootstrapConfigReconciler) machineConfigurationContext(ctx context.Context, config *bootstrapv1alpha1.TartBootstrapConfig) (bootstrap.MachineConfigurationContext, error) {
	clusterMachine, err := controller.FindCAPIMachineForBootstrap(ctx, r.Client, config)
	if err != nil {
		return bootstrap.MachineConfigurationContext{}, err
	}
	cluster := &clusterv1.Cluster{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: clusterMachine.Spec.ClusterName}, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return bootstrap.MachineConfigurationContext{}, errBootstrapContextUnavailable
		}
		return bootstrap.MachineConfigurationContext{}, err
	}
	clusterRef := cluster.Spec.InfrastructureRef
	if clusterRef.APIGroup != infrav1alpha1.GroupVersion.Group || clusterRef.Kind != controller.TartClusterKind || clusterRef.Name == "" {
		return bootstrap.MachineConfigurationContext{}, errBootstrapContextUnavailable
	}

	providerCluster := &infrav1alpha1.TartCluster{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: clusterRef.Name}, providerCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return bootstrap.MachineConfigurationContext{}, errBootstrapContextUnavailable
		}
		return bootstrap.MachineConfigurationContext{}, err
	}
	if providerCluster.Spec.ClusterID == "" || providerCluster.Status.ActiveSecretGeneration < 1 {
		return bootstrap.MachineConfigurationContext{}, errBootstrapContextUnavailable
	}
	clusterID, err := clusterdomain.ParseClusterID(providerCluster.Spec.ClusterID)
	if err != nil {
		return bootstrap.MachineConfigurationContext{}, errBootstrapContextUnavailable
	}
	if !cluster.Spec.ControlPlaneEndpoint.IsValid() || clusterMachine.Spec.Version == "" {
		return bootstrap.MachineConfigurationContext{}, errBootstrapContextUnavailable
	}
	bundleName, err := domaincontrolplane.BundleName(providerCluster.Name, clusterID, providerCluster.Status.ActiveSecretGeneration)
	if err != nil {
		return bootstrap.MachineConfigurationContext{}, errBootstrapContextUnavailable
	}
	bundleSecret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: providerCluster.Namespace, Name: bundleName}, bundleSecret); err != nil {
		if apierrors.IsNotFound(err) {
			return bootstrap.MachineConfigurationContext{}, errBootstrapContextUnavailable
		}
		return bootstrap.MachineConfigurationContext{}, err
	}
	if err := domaincontrolplane.ValidateBundleSecretContract(bundleSecret, providerCluster.Namespace, providerCluster.Name, clusterID, providerCluster.Status.ActiveSecretGeneration, domaincontrolplane.BundleStateActive, providerCluster.UID); err != nil {
		return bootstrap.MachineConfigurationContext{}, errBootstrapContextUnavailable
	}
	bundle, err := certbuilder.DecodeBundleData(bundleSecret.Data, clusterID)
	if err != nil {
		return bootstrap.MachineConfigurationContext{}, errBootstrapContextUnavailable
	}

	machineRole := domainbootstrap.MachineRoleWorker
	if _, ok := clusterMachine.Labels[clusterv1.MachineControlPlaneLabel]; ok {
		machineRole = domainbootstrap.MachineRoleControlPlane
	}
	disk, err := r.installDiskForMachine(ctx, clusterMachine)
	if err != nil {
		return bootstrap.MachineConfigurationContext{}, err
	}
	return bootstrap.MachineConfigurationContext{
		ClusterName:          cluster.Name,
		ControlPlaneEndpoint: cluster.Spec.ControlPlaneEndpoint.String(),
		KubernetesVersion:    clusterMachine.Spec.Version,
		MachineRole:          machineRole,
		SecretsBundle:        bundle,
		InstallDisk:          &disk,
	}, nil
}

func (r *TartBootstrapConfigReconciler) installDiskForMachine(ctx context.Context, machine *clusterv1.Machine) (domainbootstrap.InstallDisk, error) {
	if machine == nil || machine.Spec.InfrastructureRef.APIGroup != infrav1alpha1.GroupVersion.Group || machine.Spec.InfrastructureRef.Kind != controller.TartMachineKind || machine.Spec.InfrastructureRef.Name == "" {
		return domainbootstrap.InstallDisk{}, errBootstrapContextUnavailable
	}
	providerMachine := &infrav1alpha1.TartMachine{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: machine.Spec.InfrastructureRef.Name}, providerMachine); err != nil {
		if apierrors.IsNotFound(err) {
			return domainbootstrap.InstallDisk{}, errBootstrapContextUnavailable
		}
		return domainbootstrap.InstallDisk{}, err
	}
	if err := controller.ValidateProviderOwner(providerMachine, machine, clusterv1.GroupVersion.String(), controller.TartMachineKind); err != nil {
		return domainbootstrap.InstallDisk{}, err
	}
	if providerMachine.Status.HostRef == nil || providerMachine.Status.HostRef.Name == "" {
		return domainbootstrap.InstallDisk{}, errBootstrapContextUnavailable
	}
	host := &infrav1alpha1.TartHost{}
	if err := r.Get(ctx, client.ObjectKey{Name: providerMachine.Status.HostRef.Name}, host); err != nil {
		if apierrors.IsNotFound(err) {
			return domainbootstrap.InstallDisk{}, errBootstrapContextUnavailable
		}
		return domainbootstrap.InstallDisk{}, err
	}
	consumer := host.Spec.ConsumerRef
	if consumer == nil || consumer.APIVersion != infrav1alpha1.GroupVersion.String() || consumer.Kind != controller.TartMachineKind || consumer.Namespace != providerMachine.Namespace || consumer.Name != providerMachine.Name || consumer.UID != providerMachine.UID {
		return domainbootstrap.InstallDisk{}, errBootstrapContextUnavailable
	}
	allHosts := &infrav1alpha1.TartHostList{}
	if err := r.List(ctx, allHosts); err != nil {
		return domainbootstrap.InstallDisk{}, err
	}
	if hostpolicy.HasIdentityConflictForAny(allHosts.Items) {
		return domainbootstrap.InstallDisk{}, errBootstrapIdentityConflict
	}
	if host.Status.Inventory == nil || len(host.Status.Inventory.Disks) == 0 {
		return domainbootstrap.InstallDisk{}, errBootstrapContextUnavailable
	}
	disks := make([]domainbootstrap.InstallDisk, 0, len(host.Status.Inventory.Disks))
	for _, disk := range host.Status.Inventory.Disks {
		if disk.SizeBytes < 1 {
			continue
		}
		disks = append(disks, domainbootstrap.InstallDisk{
			DevicePath: disk.DevicePath,
			SizeBytes:  uint64(disk.SizeBytes),
			Model:      disk.Model,
			Serial:     disk.Serial,
			WWID:       disk.WWID,
			BusPath:    disk.BusPath,
			Transport:  disk.Transport,
			Rotational: disk.Rotational,
			ReadOnly:   disk.ReadOnly,
		})
	}
	return r.Renderer.SelectInstallDisk(disks)
}

// classifyConfigurationErrorは、r.configurationが返した非retryableなerrorをReady Conditionのreason/messageへ分類する。
func classifyConfigurationError(err error) (reason, message string) {
	switch {
	case errors.Is(err, errBootstrapIdentityConflict):
		return infrav1alpha1.ReasonIdentityConflict, "The Host inventory contains duplicated stable identity; configuration generation is stopped."
	case errors.Is(err, domainbootstrap.ErrConfigurationConflict):
		return "ConfigurationConflict", "The rendered Talos machine configuration conflicts with a provider-owned invariant."
	case errors.Is(err, domainbootstrap.ErrInstallDiskAmbiguous), errors.Is(err, domainbootstrap.ErrInstallConfigurationInvalid), errors.Is(err, bootstrap.ErrConfigurationInputAmbiguous):
		return "InstallDiskUnavailable", "The immutable configuration does not identify one safe Talos install disk."
	default:
		return "ConfigurationInvalid", "The referenced configuration Secret does not contain a complete valid Talos machine configuration."
	}
}

func (r *TartBootstrapConfigReconciler) report(ctx context.Context, config *bootstrapv1alpha1.TartBootstrapConfig, reason, message string) (ctrl.Result, error) {
	original := config.DeepCopy()
	controller.SetCondition(&config.Status.Conditions, bootstrapv1alpha1.TartBootstrapConfigReadyCondition, metav1.ConditionFalse, reason, message, config.Generation)
	config.Status.ObservedGeneration = config.Generation
	if err := r.Status().Patch(ctx, config, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TartBootstrapConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &bootstrapv1alpha1.TartBootstrapConfig{}, bootstrapConfigSecretIndex, func(obj client.Object) []string {
		config, ok := obj.(*bootstrapv1alpha1.TartBootstrapConfig)
		if !ok || config.Spec.ConfigPatchesSecretRef == nil {
			return nil
		}
		return []string{config.Spec.ConfigPatchesSecretRef.Name}
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&bootstrapv1alpha1.TartBootstrapConfig{}).
		Owns(&corev1.Secret{}).
		Watches(&infrav1alpha1.TartMachine{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllBootstrapConfigs)).
		Watches(&infrav1alpha1.TartHost{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllBootstrapConfigs)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			configs := &bootstrapv1alpha1.TartBootstrapConfigList{}
			if err := r.List(ctx, configs, client.InNamespace(obj.GetNamespace()), client.MatchingFields{bootstrapConfigSecretIndex: obj.GetName()}); err != nil {
				return nil
			}
			requests := make([]reconcile.Request, 0, len(configs.Items))
			for index := range configs.Items {
				requests = append(requests, reconcile.Request{Namespace: configs.Items[index].Namespace, Name: configs.Items[index].Name})
			}
			return requests
		})).
		Named("tartbootstrapconfig").
		Complete(r)
}

func (r *TartBootstrapConfigReconciler) enqueueAllBootstrapConfigs(ctx context.Context, _ client.Object) []reconcile.Request {
	configs := &bootstrapv1alpha1.TartBootstrapConfigList{}
	if err := r.List(ctx, configs); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(configs.Items))
	for index := range configs.Items {
		requests = append(requests, reconcile.Request{Namespace: configs.Items[index].Namespace, Name: configs.Items[index].Name})
	}
	return requests
}

const bootstrapConfigSecretIndex = ".spec.configPatchesSecretRef.name"
