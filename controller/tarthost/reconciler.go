// Package tarthostはTartHost resourceのKubernetes watchおよびreconcile entrypointを提供する。
package tarthost

import (
	"context"
	"errors"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/power"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controller"
	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
	hostusecase "github.com/walnuts1018/cluster-api-provider-tart/usecase/host"
)

const tartHostFinalizer = "tart.cluster.x-k8s.io/host-lifecycle"

// maxPowerOnAttemptsは、maintenance discoveryのためにHostへ電源投入要求(WakeOnLANマジック
// パケット送信等)を送る回数の上限である。1回のreconcileにつき最大1回試みるため、30秒間隔の
// reconcile(下記RequeueAfter)でおよそ15分間リトライし続けたら諦める計算になる。永久に
// broadcast packetを送り続けるのを避けつつ、電源投入からTalos起動までの一時的な遅延は許容する。
const maxPowerOnAttempts = 30

// TartHostReconcilerはHost identity、maintenance Talos discovery、削除時のretention gateを管理する。configuration applyはTartMachineへ委譲し、Discoveryのためのpower操作だけを担当する。
type TartHostReconciler struct {
	client.Client
	// ManagementNamespaceはRedfish credential Secretを解決するprovider管理namespaceである。TartHostのSpecからnamespaceを受け取らない。
	ManagementNamespace string
	// RecorderはIdentity Conflictなど利用者へ通知すべき事象をKubernetes Eventとして記録する。SetupWithManagerで未設定の場合はmanagerから解決する。
	Recorder record.EventRecorder
}

// NewTartHostReconcilerはclientのみを設定したTartHostReconcilerを構築する。ManagementNamespaceや
// Recorderなどのoptional fieldはDI wiringの呼び出し元が必要に応じて後から設定する。
func NewTartHostReconciler(c client.Client) *TartHostReconciler {
	return &TartHostReconciler{Client: c}
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

func (r *TartHostReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var current infrav1alpha1.TartHost
	if err := r.Get(ctx, req.NamespacedName, &current); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if controller.IsPaused(&current) {
		return ctrl.Result{}, nil
	}

	if !current.DeletionTimestamp.IsZero() {
		return r.reconcileDeletion(ctx, &current)
	}
	if current.Spec.HostID == "" {
		original := current.DeepCopy()
		current.Spec.HostID = hostdomain.NewHostID().String()
		if err := r.Patch(ctx, &current, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	if !controllerutil.ContainsFinalizer(&current, tartHostFinalizer) {
		original := current.DeepCopy()
		controllerutil.AddFinalizer(&current, tartHostFinalizer)
		if err := r.Patch(ctx, &current, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	hosts := &infrav1alpha1.TartHostList{}
	if err := r.List(ctx, hosts); err != nil {
		return ctrl.Result{}, err
	}
	if hostusecase.HasIdentityConflictForAny(hosts.Items) {
		return r.reportIdentityConflicts(ctx, hosts.Items)
	}

	eligibility := hostusecase.Classify(current.Spec)
	original := current.DeepCopy()
	endpoint := controller.HostTalosEndpoint(&current)
	var observationErr error
	if needsPowerOnForDiscovery(&current) {
		if powerOnRetriesExhausted(&current) {
			observationErr = errPowerOnRetriesExhausted
		} else {
			current.Status.PowerOnAttempts = recordPowerOnAttempt(current.Status.PowerOnAttempts, metav1.Now())
			if err := power.PowerOnHost(ctx, r.Client, r.ManagementNamespace, &current); err != nil {
				ctrl.LoggerFrom(ctx).Error(err, "power on Host for maintenance discovery")
				observationErr = errHostPowerUnavailable
			}
		}
	}
	var inventory talos.Inventory
	if observationErr == nil {
		inventory, observationErr = observeHost(ctx, &current)
	}
	if observationErr == nil {
		// Talosへ到達できた=電源投入が奏功したとみなせるため、次に電源が失われたときのために
		// リトライ予算をリセットする。
		current.Status.PowerOnAttempts = nil
		current.Status.Inventory = hostInventory(inventory)
		current.Status.BootAttempts = recordBootAttempt(current.Status.BootAttempts, inventory, endpoint, metav1.Now())
		if endpoint != "" {
			current.Status.Addresses = controller.HostAddresses(endpoint)
		}
		observedHosts := make([]infrav1alpha1.TartHost, 0, len(hosts.Items)+1)
		observedHosts = append(observedHosts, hosts.Items...)
		foundCurrent := false
		for index := range observedHosts {
			if observedHosts[index].Name == current.Name && observedHosts[index].Namespace == current.Namespace {
				observedHosts[index] = current
				foundCurrent = true
				break
			}
		}
		if !foundCurrent {
			observedHosts = append(observedHosts, current)
		}
		if hostusecase.HasIdentityConflictForAny(observedHosts) {
			return r.reportIdentityConflicts(ctx, observedHosts)
		}
	}
	setEligibilityConditions(&current, eligibility)
	if observationErr == nil {
		controller.SetCondition(&current.Status.Conditions, infrav1alpha1.TartHostTalosReachableCondition, metav1.ConditionTrue, "TalosReachable", "The Talos maintenance API is reachable and the Host identity matches.", current.Generation)
		controller.SetCondition(&current.Status.Conditions, infrav1alpha1.TartHostReadyCondition, metav1.ConditionTrue, "Ready", "The Host inventory and Talos maintenance endpoint are available.", current.Generation)
	} else {
		reason := "TalosUnavailable"
		message := "The Talos maintenance API is not reachable; allocation remains independent from this observation."
		if errors.Is(observationErr, errHostPowerUnavailable) {
			reason = "PowerUnavailable"
			message = "The Host could not be powered on for maintenance discovery."
		}
		if errors.Is(observationErr, errPowerOnRetriesExhausted) {
			reason = "PowerOnRetriesExhausted"
			message = "The Host power-on retry budget for maintenance discovery is exhausted; manual intervention is required."
		}
		if errors.Is(observationErr, controller.ErrHostEndpointUnavailable) {
			reason = "EndpointUnavailable"
			message = "A Talos endpoint is not configured or observed for this Host."
		}
		if errors.Is(observationErr, controller.ErrHostIdentityMismatch) {
			reason = infrav1alpha1.ReasonIdentityConflict
			message = "The Talos maintenance MAC address does not match the Host enrollment identity."
		}
		controller.SetCondition(&current.Status.Conditions, infrav1alpha1.TartHostTalosReachableCondition, metav1.ConditionFalse, reason, message, current.Generation)
		controller.SetCondition(&current.Status.Conditions, infrav1alpha1.TartHostReadyCondition, metav1.ConditionFalse, reason, message, current.Generation)
	}
	current.Status.ObservedGeneration = current.Generation
	if err := r.Status().Patch(ctx, &current, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func needsPowerOnForDiscovery(host *infrav1alpha1.TartHost) bool {
	if host == nil {
		return false
	}
	switch host.Spec.Power.Backend {
	case infrav1alpha1.PowerBackendWakeOnLAN, infrav1alpha1.PowerBackendRedfish, infrav1alpha1.PowerBackendIntelManageability:
	case infrav1alpha1.PowerBackendManual:
		return false
	default:
		return false
	}
	if host.Status.Inventory == nil {
		return true
	}
	ready := meta.FindStatusCondition(host.Status.Conditions, infrav1alpha1.TartHostReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		return true
	}
	// retentionではInventoryを保持したままgenerationが変わるため、直前Machine停止後に再検出し、古いInventoryを稼働証拠として扱わない。
	return host.Spec.PreviousConsumerRef != nil && host.Status.ObservedGeneration < host.Generation
}

// powerOnRetriesExhaustedは、直前のTalosReachable観測(=電源投入の成功とみなせる)以降に
// 送信した電源投入要求の回数がmaxPowerOnAttemptsへ達しているかを返す。
func powerOnRetriesExhausted(host *infrav1alpha1.TartHost) bool {
	return host.Status.PowerOnAttempts != nil && host.Status.PowerOnAttempts.Count >= maxPowerOnAttempts
}

// recordPowerOnAttemptは、電源投入要求(WakeOnLANマジックパケット送信等)を送信した事実を
// 観測履歴として積み増す。送信自体の成否に関わらず、実際に要求を送ったことを記録する
// (fire-and-forgetなWakeOnLANでは送信成功を電源投入成功の証明として扱えないため)。
func recordPowerOnAttempt(existing *infrav1alpha1.PowerOnAttemptStatus, now metav1.Time) *infrav1alpha1.PowerOnAttemptStatus {
	count := int32(1)
	if existing != nil {
		count = existing.Count + 1
	}
	return &infrav1alpha1.PowerOnAttemptStatus{Count: count, LastAttemptAt: now}
}

func hostInventory(inventory talos.Inventory) *infrav1alpha1.HostInventory {
	result := &infrav1alpha1.HostInventory{
		BootID:            inventory.BootID,
		SystemUUID:        inventory.SystemUUID.String(),
		Architecture:      inventory.Architecture,
		Disks:             make([]infrav1alpha1.DiskInventory, 0, len(inventory.Disks)),
		NetworkInterfaces: make([]infrav1alpha1.NetworkInterfaceInventory, 0, len(inventory.NetworkInterfaces)),
	}
	identities := make([]domainbootstrap.DiskIdentity, 0, len(inventory.Disks))
	for _, disk := range inventory.Disks {
		identities = append(identities, domainbootstrap.DiskIdentity{
			DevicePath: disk.DevicePath,
			SizeBytes:  disk.SizeBytes,
			Model:      disk.Model,
			Serial:     disk.Serial,
			WWID:       disk.WWID,
			BusPath:    disk.BusPath,
			Transport:  disk.Transport,
			Rotational: disk.Rotational,
			ReadOnly:   disk.ReadOnly,
		})
	}
	for index, disk := range inventory.Disks {
		stableSelector, _ := domainbootstrap.UniqueDiskSelector(identities[index], identities)
		result.Disks = append(result.Disks, infrav1alpha1.DiskInventory{
			DevicePath:     disk.DevicePath,
			SizeBytes:      int64(disk.SizeBytes),
			Model:          disk.Model,
			Serial:         disk.Serial,
			WWID:           disk.WWID,
			BusPath:        disk.BusPath,
			Transport:      disk.Transport,
			Rotational:     disk.Rotational,
			ReadOnly:       disk.ReadOnly,
			Symlinks:       append([]string(nil), disk.Symlinks...),
			StableSelector: stableSelector,
		})
	}
	for _, networkInterface := range inventory.NetworkInterfaces {
		result.NetworkInterfaces = append(result.NetworkInterfaces, infrav1alpha1.NetworkInterfaceInventory{
			Name:       networkInterface.Name,
			MACAddress: networkInterface.MACAddress,
			LinkState:  networkInterface.LinkState,
			Driver:     networkInterface.Driver,
			BusPath:    networkInterface.BusPath,
			Addresses:  append([]string(nil), networkInterface.Addresses...),
		})
	}
	return result
}

const maxBootAttempts = 16

func recordBootAttempt(attempts []infrav1alpha1.BootAttempt, inventory talos.Inventory, endpoint string, observedAt metav1.Time) []infrav1alpha1.BootAttempt {
	bootID := strings.TrimSpace(inventory.BootID)
	if bootID == "" {
		return attempts
	}
	for index := range attempts {
		if attempts[index].BootID != bootID {
			continue
		}
		attempts[index].LastObservedAt = observedAt
		attempts[index].SystemUUID = inventory.SystemUUID.String()
		attempts[index].Endpoint = endpoint
		return attempts
	}
	attempts = append(attempts, infrav1alpha1.BootAttempt{
		BootID:          bootID,
		FirstObservedAt: observedAt,
		LastObservedAt:  observedAt,
		SystemUUID:      inventory.SystemUUID.String(),
		Endpoint:        endpoint,
	})
	if len(attempts) > maxBootAttempts {
		attempts = attempts[len(attempts)-maxBootAttempts:]
	}
	return attempts
}

var errHostPowerUnavailable = errors.New("host power-on is unavailable")

var errPowerOnRetriesExhausted = errors.New("host power-on retry budget is exhausted")

func observeHost(ctx context.Context, current *infrav1alpha1.TartHost) (talos.Inventory, error) {
	endpoint := controller.HostTalosEndpoint(current)
	if endpoint == "" {
		return talos.Inventory{}, controller.ErrHostEndpointUnavailable
	}
	connectionContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	maintenance, err := talos.DialMaintenance(connectionContext, endpoint)
	if err != nil {
		cancel()
		return talos.Inventory{}, err
	}
	inventory, err := maintenance.Inventory(connectionContext)
	cancel()
	if closeErr := maintenance.Close(); closeErr != nil {
		ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
	}
	if err != nil {
		return talos.Inventory{}, err
	}
	if !inventory.HasMAC(current.Spec.MACAddress) {
		return talos.Inventory{}, controller.ErrHostIdentityMismatch
	}
	return inventory, nil
}

func setEligibilityConditions(hostObject *infrav1alpha1.TartHost, eligibility hostdomain.Eligibility) {
	generation := hostObject.Generation
	switch eligibility {
	case hostdomain.Available:
		controller.SetCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostAvailableCondition, metav1.ConditionTrue, infrav1alpha1.ReasonAvailable, "The Host has no active consumerRef and no retained state.", generation)
	case hostdomain.Claimed:
		controller.SetCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostAvailableCondition, metav1.ConditionFalse, infrav1alpha1.ReasonClaimed, "The Host is claimed by a TartMachine.", generation)
	case hostdomain.Retained:
		controller.SetCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostAvailableCondition, metav1.ConditionFalse, infrav1alpha1.ReasonRetained, "The Host retains state from a previous TartMachine; a matching reuse approval and reuse mode are required.", generation)
	case hostdomain.Reusable:
		controller.SetCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostAvailableCondition, metav1.ConditionFalse, infrav1alpha1.ReasonReuseApprovalRequired, "The Host retains state from a previous TartMachine but has an explicit matching reuse approval; allocation follows reuseMode, not the normal claim path.", generation)
	default:
		controller.SetCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostAvailableCondition, metav1.ConditionFalse, infrav1alpha1.ReasonRetained, "The Host eligibility could not be classified.", generation)
	}

	if hostObject.Status.Inventory != nil {
		controller.SetCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostInventoryReadyCondition, metav1.ConditionTrue, "InventoryObserved", "Hardware inventory has been observed.", generation)
	} else {
		controller.SetCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostInventoryReadyCondition, metav1.ConditionFalse, "InventoryUnavailable", "Hardware inventory has not been observed yet.", generation)
	}
}

func (r *TartHostReconciler) reconcileDeletion(ctx context.Context, current *infrav1alpha1.TartHost) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(current, tartHostFinalizer) {
		return ctrl.Result{}, nil
	}
	if !deletionApproved(current.Spec) {
		return r.report(ctx, current, infrav1alpha1.ReasonDeletionApprovalRequired, "The Host is claimed or retained; matching deletion approval is required and no power or data operation is performed.")
	}
	original := current.DeepCopy()
	controllerutil.RemoveFinalizer(current, tartHostFinalizer)
	if err := r.Patch(ctx, current, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func deletionApproved(spec infrav1alpha1.TartHostSpec) bool {
	if spec.ConsumerRef == nil && spec.PreviousConsumerRef == nil {
		return true
	}
	if spec.DeletionApproval == nil {
		return false
	}
	if spec.ConsumerRef != nil {
		if spec.ConsumerRef.UID == "" || spec.DeletionApproval.ConsumerUID == "" || spec.DeletionApproval.ConsumerUID != spec.ConsumerRef.UID {
			return false
		}
	}
	if spec.PreviousConsumerRef != nil {
		if spec.PreviousConsumerRef.UID == "" || spec.DeletionApproval.PreviousConsumerUID == "" || spec.DeletionApproval.PreviousConsumerUID != spec.PreviousConsumerRef.UID {
			return false
		}
	}
	return true
}

func (r *TartHostReconciler) report(ctx context.Context, current *infrav1alpha1.TartHost, reason, message string) (ctrl.Result, error) {
	original := current.DeepCopy()
	controller.SetCondition(&current.Status.Conditions, infrav1alpha1.TartHostReadyCondition, metav1.ConditionFalse, reason, message, current.Generation)
	current.Status.ObservedGeneration = current.Generation
	if err := r.Status().Patch(ctx, current, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TartHostReconciler) reportIdentityConflicts(ctx context.Context, hosts []infrav1alpha1.TartHost) (ctrl.Result, error) {
	for index := range hosts {
		candidate := &hosts[index]
		if controller.IsPaused(candidate) || !hostusecase.HasIdentityConflict(*candidate, hosts) {
			continue
		}

		reason := infrav1alpha1.ReasonIdentityConflict
		message := "Stable Host identity (MAC address or system UUID) is duplicated; allocation and maintenance configuration are stopped."
		if hostusecase.HasDiskIdentityConflict(*candidate, hosts) {
			reason = infrav1alpha1.ReasonDiskIdentityConflict
			message = "This Host reports a disk identity (WWID or serial) that is already reported by another Host; allocation and maintenance configuration are stopped until the conflict is resolved."
		}

		// cross-host status writeの競合を避けるため、patch前にfreshなresourceVersionを取得する。List snapshotのstale copyを直接patchしない。
		fresh := &infrav1alpha1.TartHost{}
		if err := r.Get(ctx, client.ObjectKey{Name: candidate.Name}, fresh); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return ctrl.Result{}, err
		}
		original := fresh.DeepCopy()
		controller.SetCondition(&fresh.Status.Conditions, infrav1alpha1.TartHostReadyCondition, metav1.ConditionFalse, reason, message, fresh.Generation)
		fresh.Status.ObservedGeneration = fresh.Generation
		if err := r.Status().Patch(ctx, fresh, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			if apierrors.IsConflict(err) {
				return ctrl.Result{RequeueAfter: time.Second}, nil
			}
			return ctrl.Result{}, err
		}
		if r.Recorder != nil {
			r.Recorder.Event(fresh, corev1.EventTypeWarning, reason, message)
		}
	}

	return ctrl.Result{}, nil
}

func (r *TartHostReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("tarthost-controller") //nolint:staticcheck // record.EventRecorderを型として使う既存箇所と合わせるため、新APIへの移行は別途まとめて行う
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1alpha1.TartHost{}).
		Named("tarthost").
		Complete(r)
}
