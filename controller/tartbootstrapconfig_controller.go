package controller

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

	talosmachine "github.com/siderolabs/talos/pkg/machinery/config/machine"
	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/bootstrap"
	"github.com/walnuts1018/cluster-api-provider-tart/controlplane"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TartBootstrapConfigReconciler reconciles a TartBootstrapConfig object.
type TartBootstrapConfigReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters,verbs=get;list;watch

func (r *TartBootstrapConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var config bootstrapv1alpha1.TartBootstrapConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if isPaused(&config) {
		return ctrl.Result{}, nil
	}

	if config.Spec.ConfigPatchesSecretRef == nil {
		return r.report(ctx, &config, "ConfigurationSecretUnavailable", "An immutable configuration Secret reference is required before Bootstrap data can be generated.")
	}

	input := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: config.Spec.ConfigPatchesSecretRef.Name}, input); err != nil {
		if apierrors.IsNotFound(err) {
			return r.report(ctx, &config, "ConfigurationSecretUnavailable", "The referenced immutable configuration Secret is not available.")
		}
		return ctrl.Result{}, err
	}
	completeConfiguration, err := r.configuration(ctx, &config, input)
	if err != nil {
		if errors.Is(err, errCAPIMachineUnavailable) || errors.Is(err, errBootstrapContextUnavailable) {
			result, reportErr := r.report(ctx, &config, "ClusterContextUnavailable", "The CAPI Cluster and active Talos secret bundle are not available for configuration generation yet.")
			if reportErr != nil {
				return ctrl.Result{}, reportErr
			}
			result.RequeueAfter = 15 * time.Second
			return result, nil
		}
		if errors.Is(err, bootstrap.ErrConfigurationConflict) {
			return r.report(ctx, &config, "ConfigurationConflict", "The rendered Talos machine configuration conflicts with a provider-owned invariant.")
		}
		return r.report(ctx, &config, "ConfigurationInvalid", "The referenced configuration Secret does not contain a complete valid Talos machine configuration.")
	}
	digest, err := bootstrap.DigestEffectiveConfiguration(completeConfiguration)
	if err != nil {
		return r.report(ctx, &config, "ConfigurationInvalid", "The rendered Talos machine configuration is not valid for boot.")
	}

	clusterName := config.Labels[bootstrap.ClusterNameLabel]
	if clusterName == "" {
		return r.report(ctx, &config, "ClusterNameUnavailable", "The cluster.x-k8s.io/cluster-name label is required to create the Bootstrap Secret.")
	}
	owner := metav1.OwnerReference{
		APIVersion: bootstrapv1alpha1.GroupVersion.String(),
		Kind:       tartBootstrapConfigKind,
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
			return r.report(ctx, &config, "BootstrapSecretImmutable", "The Bootstrap Secret already contains different immutable data; create a new BootstrapConfig instead.")
		}
	}

	// Bootstrap Secretは一度作成したら書き換えず、同じdesired stateの再concileでは
	// 既存Secretを観測してStatusだけを更新する。
	original := config.DeepCopy()
	config.Status.Initialization.DataSecretCreated = new(true)
	config.Status.DataSecretName = actual.Name
	config.Status.ConfigurationDigest = digest
	setCondition(&config.Status.Conditions, bootstrapv1alpha1.TartBootstrapConfigReadyCondition, metav1.ConditionTrue, "DataSecretCreated", "The immutable Bootstrap Secret is available.", config.Generation)
	config.Status.ObservedGeneration = config.Generation
	if err := r.Status().Patch(ctx, &config, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

var errBootstrapContextUnavailable = errors.New("bootstrap cluster context is unavailable")

func (r *TartBootstrapConfigReconciler) configuration(ctx context.Context, config *bootstrapv1alpha1.TartBootstrapConfig, input *corev1.Secret) ([]byte, error) {
	if err := bootstrap.ValidateConfigSecret(input); err != nil {
		return nil, err
	}
	// valueは既存の完全configuration契約を維持する。patchesだけを持つSecretだけが
	// cluster bundleとCAPI contextからの生成経路へ進む。
	if len(input.Data[bootstrap.ConfigurationInputKey]) > 0 || len(input.Data[bootstrap.ConfigurationPatchesKey]) == 0 {
		return bootstrap.CompleteConfigurationFromSecret(input)
	}

	clusterMachine, err := findCAPIMachineForBootstrap(ctx, r.Client, config)
	if err != nil {
		return nil, err
	}
	cluster := &clusterv1.Cluster{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: clusterMachine.Spec.ClusterName}, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errBootstrapContextUnavailable
		}
		return nil, err
	}
	clusterRef := cluster.Spec.InfrastructureRef
	if clusterRef.APIGroup != infrav1alpha1.GroupVersion.Group || clusterRef.Kind != tartClusterKind || clusterRef.Name == "" {
		return nil, errBootstrapContextUnavailable
	}

	providerCluster := &infrav1alpha1.TartCluster{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: clusterRef.Name}, providerCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errBootstrapContextUnavailable
		}
		return nil, err
	}
	if providerCluster.Spec.ClusterID == "" || providerCluster.Status.ActiveSecretGeneration < 1 {
		return nil, errBootstrapContextUnavailable
	}
	if !cluster.Spec.ControlPlaneEndpoint.IsValid() || clusterMachine.Spec.Version == "" {
		return nil, errBootstrapContextUnavailable
	}
	bundleName, err := controlplane.BundleName(providerCluster.Name, providerCluster.Spec.ClusterID, providerCluster.Status.ActiveSecretGeneration)
	if err != nil {
		return nil, errBootstrapContextUnavailable
	}
	bundleSecret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: providerCluster.Namespace, Name: bundleName}, bundleSecret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errBootstrapContextUnavailable
		}
		return nil, err
	}
	if err := controlplane.ValidateBundleSecretContract(bundleSecret, providerCluster.Namespace, providerCluster.Name, providerCluster.Spec.ClusterID, providerCluster.Status.ActiveSecretGeneration, controlplane.BundleStateActive, providerCluster.UID); err != nil {
		return nil, errBootstrapContextUnavailable
	}
	bundle, err := controlplane.DecodeBundleData(bundleSecret.Data, providerCluster.Spec.ClusterID)
	if err != nil {
		return nil, errBootstrapContextUnavailable
	}

	machineType := talosmachine.TypeWorker
	if _, ok := clusterMachine.Labels[clusterv1.MachineControlPlaneLabel]; ok {
		machineType = talosmachine.TypeControlPlane
	}
	return bootstrap.GenerateMachineConfiguration(bootstrap.MachineConfigurationContext{
		ClusterName:          cluster.Name,
		ControlPlaneEndpoint: cluster.Spec.ControlPlaneEndpoint.String(),
		KubernetesVersion:    clusterMachine.Spec.Version,
		MachineType:          machineType,
		SecretsBundle:        bundle,
	}, input.Data[bootstrap.ConfigurationPatchesKey])
}

func (r *TartBootstrapConfigReconciler) report(ctx context.Context, config *bootstrapv1alpha1.TartBootstrapConfig, reason, message string) (ctrl.Result, error) {
	original := config.DeepCopy()
	setCondition(&config.Status.Conditions, bootstrapv1alpha1.TartBootstrapConfigReadyCondition, metav1.ConditionFalse, reason, message, config.Generation)
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

const bootstrapConfigSecretIndex = ".spec.configPatchesSecretRef.name"
