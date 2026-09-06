package talosrecovery

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	recoveryusecase "github.com/walnuts1018/cluster-api-provider-tart/usecase/recovery"
)

// talosRecoveryResyncはrecovery Secretの参照状況を定期的に再観測する間隔である。
// 参照countのような壊れやすい状態を持たず、現在のTartHost集合の観測だけで削除可否を判断するため、定期的なresyncで収束させる。
const talosRecoveryResync = 10 * time.Minute

// TalosRecoveryReconcilerはprovider管理namespace上のTalos recovery Secretの寿命を管理する。
// TartClusterやMachineのOwnerReferenceでGCせず、少なくとも1台のTartHostがその旧Talos installationを保持している間はSecretを保持する。
type TalosRecoveryReconciler struct {
	client.Client
	// ManagementNamespaceはrecovery Secretを配置するprovider管理namespaceである。
	ManagementNamespace string
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch

func (r *TalosRecoveryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if r.ManagementNamespace == "" || req.Namespace != r.ManagementNamespace {
		return ctrl.Result{}, nil
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, req.NamespacedName, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if !recoveryusecase.IsRecoverySecret(secret) {
		return ctrl.Result{}, nil
	}

	hosts := &infrav1alpha1.TartHostList{}
	if err := r.List(ctx, hosts); err != nil {
		return ctrl.Result{}, err
	}
	if !recoveryusecase.ShouldDelete(secret, hosts.Items, time.Now(), recoveryusecase.CreationGracePeriod) {
		return ctrl.Result{RequeueAfter: talosRecoveryResync}, nil
	}
	// 最後のHostがReprovisionを完了したか、inventoryから明示的にforgetされた後だけ削除する。
	if err := r.Delete(ctx, secret, client.Preconditions{UID: &secret.UID, ResourceVersion: &secret.ResourceVersion}); err != nil && !apierrors.IsNotFound(err) && !apierrors.IsConflict(err) {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TalosRecoveryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	recoverySecrets := predicate.NewPredicateFuncs(func(object client.Object) bool {
		secret, ok := object.(*corev1.Secret)
		return ok && recoveryusecase.IsRecoverySecret(secret)
	})
	return ctrl.NewControllerManagedBy(mgr).
		// TartHostのwatchは張らず、定期的なresyncで現在の参照状況を再観測して収束させる。
		// Hostが参照をやめる遷移(Reprovision完了やHostのforget)はwatchのeventだけでは表現できず、参照countを持つと壊れやすいためである。
		For(&corev1.Secret{}, builder.WithPredicates(recoverySecrets)).
		Named("talosrecovery").
		Complete(r)
}
