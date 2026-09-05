package controller

import (
	"bytes"
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/bootstrap"
)

// TartBootstrapConfigReconciler reconciles a TartBootstrapConfig object.
type TartBootstrapConfigReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create

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

	if config.Spec.ConfigSecretRef == nil {
		return r.report(ctx, &config, "ConfigurationSecretUnavailable", "An immutable configuration Secret reference is required before Bootstrap data can be generated.")
	}

	input := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: config.Spec.ConfigSecretRef.Name}, input); err != nil {
		if apierrors.IsNotFound(err) {
			return r.report(ctx, &config, "ConfigurationSecretUnavailable", "The referenced immutable configuration Secret is not available.")
		}
		return ctrl.Result{}, err
	}
	completeConfiguration, err := bootstrap.CompleteConfigurationFromSecret(input)
	if err != nil {
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
	config.Status.DataSecretName = new(actual.Name)
	config.Status.ConfigurationDigest = digest
	setCondition(&config.Status.Conditions, bootstrapv1alpha1.TartBootstrapConfigReadyCondition, metav1.ConditionTrue, "DataSecretCreated", "The immutable Bootstrap Secret is available.", config.Generation)
	config.Status.ObservedGeneration = config.Generation
	if err := r.Status().Patch(ctx, &config, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
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
		if !ok || config.Spec.ConfigSecretRef == nil {
			return nil
		}
		return []string{config.Spec.ConfigSecretRef.Name}
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

const bootstrapConfigSecretIndex = ".spec.configSecretRef.name"
