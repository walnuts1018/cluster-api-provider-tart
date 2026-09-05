// Package controllerはTart resourceのKubernetes watchおよびreconcile entrypointを提供する。
package controller

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
	"github.com/walnuts1018/cluster-api-provider-tart/host"
	"github.com/walnuts1018/cluster-api-provider-tart/talos"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

const tartHostFinalizer = "tart.cluster.x-k8s.io/host-lifecycle"

// TartHostReconcilerはHost identity、maintenance Talos discovery、削除時のretention gateを管理する。configuration applyはTartMachineへ委譲し、Discoveryのためのpower操作だけを担当する。
type TartHostReconciler struct {
	client.Client
	// ManagementNamespaceはRedfish credential Secretを解決するprovider管理namespaceである。TartHostのSpecからnamespaceを受け取らない。
	ManagementNamespace string
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
	if isPaused(&current) {
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
	if host.HasIdentityConflictForAny(hosts.Items) {
		return r.reportIdentityConflicts(ctx, hosts.Items)
	}

	eligibility := host.Classify(current.Spec)
	original := current.DeepCopy()
	endpoint := hostTalosEndpoint(&current)
	var observationErr error
	if current.Status.Inventory == nil && (current.Spec.Power.Backend == infrav1alpha1.PowerBackendWakeOnLAN || current.Spec.Power.Backend == infrav1alpha1.PowerBackendRedfish) {
		if err := r.powerOnHost(ctx, &current); err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "power on Host for maintenance discovery")
			observationErr = errHostPowerUnavailable
		}
	}
	var inventory talos.Inventory
	if observationErr == nil {
		inventory, observationErr = observeHost(ctx, &current)
	}
	if observationErr == nil {
		current.Status.Inventory = hostInventory(inventory)
		current.Status.BootAttempts = recordBootAttempt(current.Status.BootAttempts, inventory, endpoint, metav1.Now())
		if endpoint != "" {
			current.Status.Addresses = hostAddresses(endpoint)
		}
		observedHosts := make([]infrav1alpha1.TartHost, len(hosts.Items))
		copy(observedHosts, hosts.Items)
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
		if host.HasIdentityConflictForAny(observedHosts) {
			return r.reportIdentityConflicts(ctx, observedHosts)
		}
	}
	setEligibilityConditions(&current, eligibility)
	if observationErr == nil {
		setCondition(&current.Status.Conditions, infrav1alpha1.TartHostTalosReachableCondition, metav1.ConditionTrue, "TalosReachable", "The Talos maintenance API is reachable and the Host identity matches.", current.Generation)
		setCondition(&current.Status.Conditions, infrav1alpha1.TartHostReadyCondition, metav1.ConditionTrue, "Ready", "The Host inventory and Talos maintenance endpoint are available.", current.Generation)
	} else {
		reason := "TalosUnavailable"
		message := "The Talos maintenance API is not reachable; allocation remains independent from this observation."
		if errors.Is(observationErr, errHostPowerUnavailable) {
			reason = "PowerUnavailable"
			message = "The Host could not be powered on for maintenance discovery."
		}
		if errors.Is(observationErr, errHostEndpointUnavailable) {
			reason = "EndpointUnavailable"
			message = "A Talos endpoint is not configured or observed for this Host."
		}
		if errors.Is(observationErr, errHostIdentityMismatch) {
			reason = infrav1alpha1.ReasonIdentityConflict
			message = "The Talos maintenance MAC address does not match the Host enrollment identity."
		}
		setCondition(&current.Status.Conditions, infrav1alpha1.TartHostTalosReachableCondition, metav1.ConditionFalse, reason, message, current.Generation)
		setCondition(&current.Status.Conditions, infrav1alpha1.TartHostReadyCondition, metav1.ConditionFalse, reason, message, current.Generation)
	}
	current.Status.ObservedGeneration = current.Generation
	if err := r.Status().Patch(ctx, &current, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func hostInventory(inventory talos.Inventory) *infrav1alpha1.HostInventory {
	result := &infrav1alpha1.HostInventory{
		BootID:            inventory.BootID,
		SystemUUID:        inventory.SystemUUID.String(),
		Architecture:      inventory.Architecture,
		Disks:             make([]infrav1alpha1.DiskInventory, 0, len(inventory.Disks)),
		NetworkInterfaces: make([]infrav1alpha1.NetworkInterfaceInventory, 0, len(inventory.NetworkInterfaces)),
	}
	for _, disk := range inventory.Disks {
		result.Disks = append(result.Disks, infrav1alpha1.DiskInventory{
			DevicePath: disk.DevicePath,
			SizeBytes:  int64(disk.SizeBytes),
			Model:      disk.Model,
			Serial:     disk.Serial,
			WWID:       disk.WWID,
			BusPath:    disk.BusPath,
			Transport:  disk.Transport,
			Rotational: disk.Rotational,
			ReadOnly:   disk.ReadOnly,
			Symlinks:   append([]string(nil), disk.Symlinks...),
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

var errHostIdentityMismatch = errors.New("talos maintenance identity does not match host")
var errHostEndpointUnavailable = errors.New("talos endpoint is unavailable")
var errHostPowerUnavailable = errors.New("host power-on is unavailable")

func observeHost(ctx context.Context, current *infrav1alpha1.TartHost) (talos.Inventory, error) {
	endpoint := hostTalosEndpoint(current)
	if endpoint == "" {
		return talos.Inventory{}, errHostEndpointUnavailable
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
		return talos.Inventory{}, errHostIdentityMismatch
	}
	return inventory, nil
}

func hostAddresses(endpoint string) clusterv1.MachineAddresses {
	address := endpoint
	if hostPart, _, err := net.SplitHostPort(endpoint); err == nil {
		address = hostPart
	}
	address = strings.Trim(address, "[]")
	if net.ParseIP(address) != nil {
		return clusterv1.MachineAddresses{{Type: clusterv1.MachineInternalIP, Address: address}}
	}
	return clusterv1.MachineAddresses{{Type: clusterv1.MachineHostName, Address: address}}
}

func setEligibilityConditions(hostObject *infrav1alpha1.TartHost, eligibility host.Eligibility) {
	generation := hostObject.Generation
	switch eligibility {
	case host.Available:
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostAvailableCondition, metav1.ConditionTrue, infrav1alpha1.ReasonAvailable, "The Host has no active consumerRef and no retained state.", generation)
	case host.Claimed:
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostAvailableCondition, metav1.ConditionFalse, infrav1alpha1.ReasonClaimed, "The Host is claimed by a TartMachine.", generation)
	case host.Retained:
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostAvailableCondition, metav1.ConditionFalse, infrav1alpha1.ReasonRetained, "The Host retains state from a previous TartMachine; a matching reuse approval and reuse mode are required.", generation)
	case host.Reusable:
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostAvailableCondition, metav1.ConditionFalse, infrav1alpha1.ReasonReuseApprovalRequired, "The Host retains state from a previous TartMachine but has an explicit matching reuse approval; allocation follows reuseMode, not the normal claim path.", generation)
	default:
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostAvailableCondition, metav1.ConditionFalse, infrav1alpha1.ReasonRetained, "The Host eligibility could not be classified.", generation)
	}

	if hostObject.Status.Inventory != nil {
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostInventoryReadyCondition, metav1.ConditionTrue, "InventoryObserved", "Hardware inventory has been observed.", generation)
	} else {
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostInventoryReadyCondition, metav1.ConditionFalse, "InventoryUnavailable", "Hardware inventory has not been observed yet.", generation)
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
	setCondition(&current.Status.Conditions, infrav1alpha1.TartHostReadyCondition, metav1.ConditionFalse, reason, message, current.Generation)
	current.Status.ObservedGeneration = current.Generation
	if err := r.Status().Patch(ctx, current, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TartHostReconciler) reportIdentityConflicts(ctx context.Context, hosts []infrav1alpha1.TartHost) (ctrl.Result, error) {
	for index := range hosts {
		candidate := &hosts[index]
		if isPaused(candidate) || !host.HasIdentityConflict(*candidate, hosts) {
			continue
		}

		original := candidate.DeepCopy()
		setCondition(&candidate.Status.Conditions, infrav1alpha1.TartHostReadyCondition, metav1.ConditionFalse, infrav1alpha1.ReasonIdentityConflict, "Stable Host identity is duplicated; allocation and maintenance configuration are stopped.", candidate.Generation)
		candidate.Status.ObservedGeneration = candidate.Generation
		if err := r.Status().Patch(ctx, candidate, client.MergeFrom(original)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *TartHostReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1alpha1.TartHost{}).
		Named("tarthost").
		Complete(r)
}
