package tartmachine

import (
	"context"
	"errors"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controller"
)

const (
	tartMachineFinalizer        = "tart.cluster.x-k8s.io/machine-lifecycle"
	shutdownConfirmationRequeue = 30 * time.Second
	shutdownConfirmationDelay   = 30 * time.Second
	talosReconcileTimeout       = 20 * time.Second
	talosRequeue                = 30 * time.Second
	maintenanceDialTimeout      = 10 * time.Second
	maintenanceObserveTimeout   = 10 * time.Second
	maintenanceActionTimeout    = 20 * time.Second
)

var (
	ErrBootstrapDataUnavailable = errors.New("bootstrap data is unavailable")
	// ErrShutdownStateUnverifiableは、WoL/Manualなど独立したpower-state observerを持たないHostで停止検証ができないことを示す。
	ErrShutdownStateUnverifiable = errors.New("host shutdown state is unverifiable without an independent power-state observer")
	errCAPIProviderIDMismatch    = errors.New("CAPI Machine provider ID does not match TartHost")
	errHostSelectionMismatch     = errors.New("allocated TartHost does not match CAPI Machine placement")
)

// TartMachineReconcilerはHost claim、Talosの初回configuration apply、認証済みAPIの起動確認を担当する。初回provisioning後のmutableな変更はUpdate Extensionへ委譲する。
type TartMachineReconciler struct {
	client.Client
	// ManagementNamespaceはRedfish credential SecretとTalos recovery Secretを解決するprovider管理namespaceである。TartHostのSpecからnamespaceを受け取らない。
	ManagementNamespace string
	// TalosDialerはReprovision flowのTalos接続を差し替えるための境界である。未設定の場合は実際のTalos gRPC clientを使う。
	TalosDialer TalosDialer
}

// NewTartMachineReconcilerはclientのみを設定したTartMachineReconcilerを構築する。ManagementNamespaceや
// TalosDialerなどのoptional fieldはDI wiringの呼び出し元が必要に応じて後から設定する。
func NewTartMachineReconciler(c client.Client) *TartMachineReconciler {
	return &TartMachineReconciler{Client: c}
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create

func (r *TartMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var machine infrav1alpha1.TartMachine
	if err := r.Get(ctx, req.NamespacedName, &machine); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if controller.IsPaused(&machine) {
		return ctrl.Result{}, nil
	}

	if !machine.DeletionTimestamp.IsZero() {
		return r.reconcileDeletion(ctx, &machine)
	}
	if !controllerutil.ContainsFinalizer(&machine, tartMachineFinalizer) {
		original := machine.DeepCopy()
		controllerutil.AddFinalizer(&machine, tartMachineFinalizer)
		if err := r.Patch(ctx, &machine, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	return r.reconcileProvisioning(ctx, &machine)
}

func (r *TartMachineReconciler) report(ctx context.Context, machine *infrav1alpha1.TartMachine, reason, message string) error {
	original := machine.DeepCopy()
	controller.SetCondition(&machine.Status.Conditions, infrav1alpha1.TartMachineReadyCondition, metav1.ConditionFalse, reason, message, machine.Generation)
	machine.Status.ObservedGeneration = machine.Generation
	if err := r.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return err
	}
	return nil
}

func (r *TartMachineReconciler) reportAndRequeue(ctx context.Context, machine *infrav1alpha1.TartMachine, reason, message string, after time.Duration) (ctrl.Result, error) {
	err := r.report(ctx, machine, reason, message)
	return ctrl.Result{RequeueAfter: after}, err
}

func (r *TartMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1alpha1.TartMachine{}).
		Watches(&infrav1alpha1.TartHost{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
			hostObject, ok := obj.(*infrav1alpha1.TartHost)
			if !ok || hostObject.Spec.ConsumerRef == nil || hostObject.Spec.ConsumerRef.Namespace == "" || hostObject.Spec.ConsumerRef.Name == "" || hostObject.Spec.ConsumerRef.UID == "" {
				return nil
			}
			return []reconcile.Request{{Namespace: hostObject.Spec.ConsumerRef.Namespace, Name: hostObject.Spec.ConsumerRef.Name}}
		})).
		Named("tartmachine").
		Complete(r)
}
