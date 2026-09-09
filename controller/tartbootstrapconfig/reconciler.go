package tartbootstrapconfig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
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

	input, reason, message, err := r.configurationInput(ctx, &config)
	if reason != "" {
		return r.report(ctx, &config, reason, message)
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	completeConfiguration, err := r.configuration(ctx, &config, input)
	if err != nil {
		return r.reportConfigurationError(ctx, &config, err)
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
	secretName := bootstrapSecretName(config.Name, digest)
	expected, err := bootstrap.BuildSecret(config.Namespace, secretName, clusterName, owner, completeConfiguration)
	if err != nil {
		return r.report(ctx, &config, "BootstrapSecretInvalid", "The Bootstrap Secret owner or metadata cannot satisfy the CAPI contract.")
	}

	actual, reason, message, result, err := r.ensureBootstrapSecret(ctx, &config, expected, clusterName, secretName, completeConfiguration)
	if reason != "" {
		return r.report(ctx, &config, reason, message)
	}
	if err != nil {
		return result, err
	}
	if actual == nil {
		return result, nil
	}

	return r.markReady(ctx, &config, actual, digest)
}

func (r *TartBootstrapConfigReconciler) configurationInput(ctx context.Context, config *bootstrapv1alpha1.TartBootstrapConfig) (*corev1.Secret, string, string, error) {
	if config.Spec.ConfigPatchesSecretRef == nil {
		return nil, "", "", nil
	}
	if config.Spec.ConfigPatchesSecretRef.Name == "" {
		return nil, "ConfigurationSecretUnavailable", "The configuration Secret reference has no name.", errors.New("configuration Secret reference has no name")
	}
	input := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: config.Spec.ConfigPatchesSecretRef.Name}, input); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, "ConfigurationSecretUnavailable", "The referenced immutable configuration Secret is not available.", err
		}
		return nil, "", "", err
	}
	return input, "", "", nil
}

func (r *TartBootstrapConfigReconciler) reportConfigurationError(ctx context.Context, config *bootstrapv1alpha1.TartBootstrapConfig, err error) (ctrl.Result, error) {
	if errors.Is(err, controller.ErrCAPIMachineUnavailable) || errors.Is(err, errBootstrapContextPending) || errors.Is(err, errBootstrapInventoryPending) || errors.Is(err, errBootstrapContextUnavailable) {
		result, reportErr := r.report(ctx, config, "ClusterContextPending", "Bootstrap configuration is waiting for required observed state.")
		if reportErr != nil {
			return ctrl.Result{}, reportErr
		}
		result.RequeueAfter = 15 * time.Second
		return result, nil
	}
	if errors.Is(err, errBootstrapBindingMismatch) || errors.Is(err, errBootstrapHostUnavailable) {
		return r.report(ctx, config, "HostBindingMismatch", "The TartHost binding does not match the CAPI/TartMachine identity; configuration generation is stopped.")
	}
	if errors.Is(err, domainbootstrap.ErrDiskSelectionUnavailable) || errors.Is(err, domainbootstrap.ErrInstallDiskUnavailable) || errors.Is(err, errBootstrapDiskUnavailable) {
		return r.report(ctx, config, "InstallDiskUnavailable", "No writable disk with a safe stable identity is available for Talos installation.")
	}
	if errors.Is(err, domainbootstrap.ErrDiskSelectionAmbiguous) || errors.Is(err, domainbootstrap.ErrInstallDiskAmbiguous) {
		return r.report(ctx, config, "InstallDiskAmbiguous", "Multiple writable disks are available but no explicit install disk policy is configured; installation is stopped.")
	}
	reason, message, recognized := classifyConfigurationError(err)
	if !recognized {
		// classifyConfigurationErrorのdefaultへ落ちるerrorは、既知の恒久的なmisconfiguration
		// sentinelのいずれとも一致しない。TartCluster/bundle Secret/TartHost/TartMachineの
		// GetがNotFound以外の理由で失敗した場合の生のerrorもここへ到達しうるため、恒久的な
		// ConfigurationInvalid conditionを書かず、controller-runtimeの標準的なbackoffで
		// 再試行させる(configurationInputの同種の分岐と同じ方針)。
		return ctrl.Result{}, err
	}
	return r.report(ctx, config, reason, message)
}

func (r *TartBootstrapConfigReconciler) ensureBootstrapSecret(ctx context.Context, config *bootstrapv1alpha1.TartBootstrapConfig, expected *corev1.Secret, clusterName, secretName string, completeConfiguration []byte) (*corev1.Secret, string, string, ctrl.Result, error) {
	actual := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: secretName}, actual)
	switch {
	case apierrors.IsNotFound(err):
		if err := r.Create(ctx, expected); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil, "", "", ctrl.Result{RequeueAfter: time.Nanosecond}, nil
			}
			return nil, "", "", ctrl.Result{}, err
		}
		return expected, "", "", ctrl.Result{}, nil
	case err != nil:
		return nil, "", "", ctrl.Result{}, err
	default:
		if !bootstrap.IsContractSecret(actual, clusterName, config.UID) {
			return nil, "BootstrapSecretInvalid", "The existing Bootstrap Secret does not satisfy the CAPI contract.", ctrl.Result{}, nil
		}
		if !bytes.Equal(actual.Data[bootstrap.BootstrapSecretKey], completeConfiguration) {
			return nil, "BootstrapSecretDigestMismatch", "The immutable Bootstrap Secret does not match its configuration digest.", ctrl.Result{}, nil
		}
		return actual, "", "", ctrl.Result{}, nil
	}
}

func (r *TartBootstrapConfigReconciler) markReady(ctx context.Context, config *bootstrapv1alpha1.TartBootstrapConfig, secret *corev1.Secret, digest string) (ctrl.Result, error) {
	// Bootstrap Secretはimmutableであり、同じdesired stateの再reconcileでは既存Secretを観測してStatusだけを更新する。
	// desired configurationが変わった場合の扱いはconfiguration update policyが決める。
	original := config.DeepCopy()
	config.Status.Initialization.DataSecretCreated = new(true)
	config.Status.DataSecretName = secret.Name
	config.Status.ConfigurationDigest = digest
	controller.SetCondition(&config.Status.Conditions, bootstrapv1alpha1.TartBootstrapConfigReadyCondition, metav1.ConditionTrue, "DataSecretCreated", "The immutable Bootstrap Secret is available.", config.Generation)
	config.Status.ObservedGeneration = config.Generation
	if err := r.Status().Patch(ctx, config, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

var errBootstrapContextUnavailable = errors.New("bootstrap cluster context is unavailable")
var errBootstrapIdentityConflict = errors.New("bootstrap Host identity conflict")
var errBootstrapContextPending = errors.New("bootstrap context is pending")
var errBootstrapBindingMismatch = errors.New("bootstrap host binding mismatch")
var errBootstrapInventoryPending = errors.New("host inventory is pending")
var errBootstrapDiskUnavailable = errors.New("safe install disk is unavailable")
var errBootstrapHostUnavailable = errors.New("bootstrap host is unavailable")

func (r *TartBootstrapConfigReconciler) configuration(ctx context.Context, config *bootstrapv1alpha1.TartBootstrapConfig, input *corev1.Secret) ([]byte, error) {
	if input == nil {
		return r.configurationFromPatches(ctx, config, nil)
	}
	if err := bootstrap.ValidateConfigSecret(input); err != nil {
		return nil, err
	}
	return r.configurationFromPatches(ctx, config, input.Data[bootstrap.ConfigurationPatchesKey])
}

func (r *TartBootstrapConfigReconciler) configurationFromPatches(ctx context.Context, config *bootstrapv1alpha1.TartBootstrapConfig, patches []byte) ([]byte, error) {
	configurationContext, err := r.machineConfigurationContext(ctx, config)
	if err != nil {
		return nil, err
	}
	configuration, err := bootstrap.RenderFromPatches(r.Renderer, configurationContext, patches)
	if err != nil {
		return nil, err
	}
	providerID, err := r.providerIDForBootstrapMachine(ctx, config)
	if err != nil {
		return nil, err
	}
	configuration, err = talos.SetProviderID(configuration, providerID)
	if err != nil {
		return nil, fmt.Errorf("set allocated ProviderID in bootstrap configuration: %w", err)
	}
	return configuration, nil
}

// providerIDForBootstrapMachineは、Host claim後にTartMachineへcontrollerが記録したProviderIDをbootstrap configurationへ適用するために観測する。ProviderIDが未確定な間は、空値を含むSecretを発行せず、Host allocationの観測が収束するまで待つ。
func (r *TartBootstrapConfigReconciler) providerIDForBootstrapMachine(ctx context.Context, config *bootstrapv1alpha1.TartBootstrapConfig) (string, error) {
	machine, err := controller.FindCAPIMachineForBootstrap(ctx, r.Client, config)
	if err != nil {
		return "", err
	}
	ref := machine.Spec.InfrastructureRef
	if ref.APIGroup != infrav1alpha1.GroupVersion.Group || ref.Kind != controller.TartMachineKind || ref.Name == "" {
		return "", errBootstrapContextPending
	}
	providerMachine := &infrav1alpha1.TartMachine{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: ref.Name}, providerMachine); err != nil {
		if apierrors.IsNotFound(err) {
			return "", errBootstrapContextPending
		}
		return "", err
	}
	if providerMachine.Spec.ProviderID.IsZero() {
		return "", errBootstrapContextPending
	}
	return providerMachine.Spec.ProviderID.String(), nil
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
	disks, err := r.disksForMachine(ctx, clusterMachine)
	if err != nil {
		return bootstrap.MachineConfigurationContext{}, err
	}
	// SelectDiskはwritable diskが複数存在する場合、暗黙のfallbackを避けるため意図的に
	// fail-closedでErrDiskSelectionAmbiguousを返す。MachineConfigurationContext.InstallDisk
	// はこの場合nilのままにする契約であり(usecase/bootstrap/dependencies.goのdoc参照)、
	// raw patchがinstall targetを明示することを期待して render 自体は続行する。raw patchも
	// install targetを含まなければ、EnsureInstallDisk/HasInstallDiskConfigurationが
	// 検証時にErrInstallConfigurationInvalid等で改めてfail-closedにする。
	var installDiskPtr *domainbootstrap.DiskIdentity
	installDisk, err := r.Renderer.SelectDisk(disks)
	switch {
	case err == nil:
		installDiskPtr = &installDisk
	case errors.Is(err, domainbootstrap.ErrDiskSelectionAmbiguous), errors.Is(err, domainbootstrap.ErrDiskSelectionUnavailable):
		installDiskPtr = nil
	default:
		return bootstrap.MachineConfigurationContext{}, err
	}
	return bootstrap.MachineConfigurationContext{
		ClusterName:          cluster.Name,
		ControlPlaneEndpoint: cluster.Spec.ControlPlaneEndpoint.String(),
		KubernetesVersion:    clusterMachine.Spec.Version,
		MachineRole:          machineRole,
		SecretsBundle:        bundle,
		InstallDisk:          installDiskPtr,
		Hostname:             clusterMachine.Name,
	}, nil
}

// disksForMachineは、machineがclaimしているTartHostを解決し、observed disk inventoryを
// domainbootstrap.DiskIdentityへ変換して返す。install target選択が使う共通経路である。
func (r *TartBootstrapConfigReconciler) disksForMachine(ctx context.Context, machine *clusterv1.Machine) ([]domainbootstrap.DiskIdentity, error) {
	if machine == nil || machine.Spec.InfrastructureRef.APIGroup != infrav1alpha1.GroupVersion.Group || machine.Spec.InfrastructureRef.Kind != controller.TartMachineKind || machine.Spec.InfrastructureRef.Name == "" {
		return nil, errBootstrapContextPending
	}
	providerMachine := &infrav1alpha1.TartMachine{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: machine.Spec.InfrastructureRef.Name}, providerMachine); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: TartMachine %s/%s is not available", errBootstrapContextPending, machine.Namespace, machine.Spec.InfrastructureRef.Name)
		}
		return nil, err
	}
	if err := controller.ValidateProviderOwner(providerMachine, machine, clusterv1.GroupVersion.String(), controller.CAPIMachineKind); err != nil {
		return nil, err
	}
	if providerMachine.Status.HostRef == nil || providerMachine.Status.HostRef.Name == "" {
		return nil, fmt.Errorf("%w: TartMachine %s/%s has no HostRef", errBootstrapContextPending, providerMachine.Namespace, providerMachine.Name)
	}
	host := &infrav1alpha1.TartHost{}
	if err := r.Get(ctx, client.ObjectKey{Name: providerMachine.Status.HostRef.Name}, host); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: TartHost %s is not available", errBootstrapHostUnavailable, providerMachine.Status.HostRef.Name)
		}
		return nil, err
	}
	consumer := host.Spec.ConsumerRef
	if consumer == nil || consumer.APIVersion != infrav1alpha1.GroupVersion.String() || consumer.Kind != controller.TartMachineKind || consumer.Namespace != providerMachine.Namespace || consumer.Name != providerMachine.Name || consumer.UID != providerMachine.UID {
		return nil, fmt.Errorf("%w: Host %s is not claimed by TartMachine %s/%s", errBootstrapBindingMismatch, host.Name, providerMachine.Namespace, providerMachine.Name)
	}
	allHosts := &infrav1alpha1.TartHostList{}
	if err := r.List(ctx, allHosts); err != nil {
		return nil, err
	}
	if hostpolicy.HasIdentityConflictForAny(allHosts.Items) {
		return nil, errBootstrapIdentityConflict
	}
	if host.Status.Inventory == nil {
		return nil, fmt.Errorf("%w: Host %s inventory is not observed", errBootstrapInventoryPending, host.Name)
	}
	if len(host.Status.Inventory.Disks) == 0 {
		return nil, fmt.Errorf("%w: Host %s reports no disks", errBootstrapDiskUnavailable, host.Name)
	}
	disks := make([]domainbootstrap.DiskIdentity, 0, len(host.Status.Inventory.Disks))
	for _, disk := range host.Status.Inventory.Disks {
		if disk.SizeBytes < 1 {
			continue
		}
		disks = append(disks, domainbootstrap.DiskIdentity{
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
	if len(disks) == 0 {
		return nil, fmt.Errorf("%w: Host %s has no usable disks", errBootstrapDiskUnavailable, host.Name)
	}
	return disks, nil
}

// classifyConfigurationErrorは、r.configurationが返した非retryableなerrorをReady Conditionのreason/messageへ分類する。
// recognizedがfalseの場合、errはこの関数が認識する恒久的なsentinelのいずれとも一致しない
// (APIサーバーの一時的な障害による生のerrorを含みうる)ため、呼び出し側は恒久的なconditionを
// 書かずに再試行を優先すべきである。
func classifyConfigurationError(err error) (reason, message string, recognized bool) {
	switch {
	case errors.Is(err, errBootstrapIdentityConflict):
		return infrav1alpha1.ReasonIdentityConflict, "The Host inventory contains duplicated stable identity; configuration generation is stopped.", true
	case errors.Is(err, domainbootstrap.ErrConfigurationConflict):
		return "ConfigurationConflict", "The rendered Talos machine configuration conflicts with a provider-owned invariant.", true
	case errors.Is(err, talos.ErrProviderIDConflict):
		return "ConfigurationConflict", "The rendered Talos machine configuration contains a ProviderID that conflicts with the allocated Host.", true
	case errors.Is(err, domainbootstrap.ErrDiskSelectionAmbiguous), errors.Is(err, domainbootstrap.ErrInstallConfigurationInvalid):
		return "InstallDiskUnavailable", "The immutable configuration does not identify one safe Talos install disk.", true
	default:
		return "ConfigurationInvalid", "The referenced configuration Secret does not contain a complete valid Talos machine configuration.", false
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

func bootstrapSecretName(configName, digest string) string {
	if len(digest) >= 10 {
		digest = digest[:10]
	}
	name := configName + "-" + digest
	if len(name) > 253 {
		// Kubernetes nameは253文字まで。configNameが長い場合は切り詰める。
		maxConfig := max(253-len(digest)-1, 1)
		if len(configName) > maxConfig {
			configName = configName[:maxConfig]
		}
		name = configName + "-" + digest
	}
	return name
}
