package tartmachine

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos/configbuilder"
	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controller"
	"github.com/walnuts1018/cluster-api-provider-tart/controller/runtimeextension"
	domainupdate "github.com/walnuts1018/cluster-api-provider-tart/domain/update"
)

const (
	endpointLeaseDuration = int32(15 * 60)
	endpointLeasePrefix   = "tart-control-plane-endpoint-"
)

func (r *TartMachineReconciler) reconcileMachineConfiguration(ctx context.Context, machine *infrav1alpha1.TartMachine, authenticated *talos.Client, desired []byte) runtimeextension.ConfigurationUpdateOutcome {
	active, err := authenticated.ActiveMachineConfiguration(ctx)
	if err != nil {
		return r.configurationUpdateFailure(machine, "ConfigurationObservationFailed", "The active Talos machine configuration could not be observed safely.")
	}
	activeEndpoint, err := configbuilder.ControlPlaneEndpoint(active)
	if err != nil {
		return r.configurationUpdateFailure(machine, "ConfigurationObservationFailed", "The active Talos machine configuration has no observable control-plane endpoint.")
	}
	activeDigest, err := configbuilder.DigestEffectiveConfiguration(active)
	if err != nil {
		return r.configurationUpdateFailure(machine, "ConfigurationObservationFailed", "The active Talos machine configuration digest could not be observed safely.")
	}
	machine.Status.ObservedControlPlaneEndpoint = activeEndpoint
	machine.Status.ObservedConfigurationDigest = activeDigest
	desiredDigest, err := configbuilder.DigestEffectiveConfiguration(desired)
	if err != nil {
		return r.configurationUpdateFailure(machine, "ConfigurationInvalid", "The desired Talos machine configuration digest could not be calculated safely.")
	}
	if activeDigest == desiredDigest {
		previous := meta.FindStatusCondition(machine.Status.Conditions, infrav1alpha1.TartMachineConfigurationUpToDateCondition)
		if previous != nil && previous.Status == metav1.ConditionFalse {
			capiMachine, config, configErr := r.bootstrapConfigForMachine(ctx, machine)
			if configErr != nil {
				return r.configurationUpdateFailure(machine, "BootstrapDataUnavailable", "The individual BootstrapConfig for this Machine is not available.")
			}
			outcome := runtimeextension.PerformConfigurationUpdate(ctx, r.Client, capiMachine, machine.Spec.ProviderID.String(), desired, config.Spec.EffectiveConfigurationApplyStrategy(), authenticated, nil)
			if outcome.Done {
				r.setConfigurationCondition(machine, metav1.ConditionTrue, "ConfigurationConverged", "The active Talos machine configuration matches the desired configuration and the Node recovered.")
			} else if outcome.FailureMessage != "" {
				r.setConfigurationCondition(machine, metav1.ConditionFalse, "ConfigurationUpdateFailed", outcome.FailureMessage)
			} else {
				r.setConfigurationCondition(machine, metav1.ConditionFalse, "ConfigurationUpdatePending", outcome.RetryMessage)
			}
			return outcome
		}
		r.setConfigurationCondition(machine, metav1.ConditionTrue, "ConfigurationConverged", "The active Talos machine configuration matches the desired configuration.")
		return runtimeextension.ConfigurationUpdateOutcome{Done: true}
	}
	capiMachine, config, err := r.bootstrapConfigForMachine(ctx, machine)
	if err != nil {
		return r.configurationUpdateFailure(machine, "BootstrapDataUnavailable", "The individual BootstrapConfig for this Machine is not available.")
	}
	class, _, err := configbuilder.ClassifyConfigurationChange(active, desired)
	if err != nil {
		return r.configurationUpdateFailure(machine, "ConfigurationInvalid", "The machine configuration difference could not be evaluated safely.")
	}
	var release func()
	if class == domainupdate.ChangeControlPlaneEndpoint {
		acquired, acquireErr := r.acquireEndpointLease(ctx, capiMachine)
		if acquireErr != nil {
			return r.configurationUpdateFailure(machine, "EndpointUpdateLockUnavailable", "The control-plane endpoint update lock could not be observed.")
		}
		if !acquired {
			r.setConfigurationCondition(machine, metav1.ConditionFalse, "EndpointUpdateInProgress", "Another Machine is updating the control-plane endpoint.")
			return runtimeextension.ConfigurationUpdateOutcome{RetryMessage: "Another Machine is updating the control-plane endpoint."}
		}
		released := false
		release = func() {
			if released {
				return
			}
			released = true
			if releaseErr := r.releaseEndpointLease(ctx, capiMachine); releaseErr != nil {
				ctrl.LoggerFrom(ctx).Error(releaseErr, "release control-plane endpoint update lock")
			}
		}
	}
	outcome := runtimeextension.PerformConfigurationUpdate(ctx, r.Client, capiMachine, machine.Spec.ProviderID.String(), desired, config.Spec.EffectiveConfigurationApplyStrategy(), authenticated, func(ctx context.Context) (bool, string) {
		if class != domainupdate.ChangeControlPlaneEndpoint {
			return true, ""
		}
		allowed, message := r.endpointUpdateGate(ctx, capiMachine, desiredEndpoint(desired))
		if !allowed && release != nil {
			release()
		}
		return allowed, message
	})
	if outcome.Done {
		r.setConfigurationCondition(machine, metav1.ConditionTrue, "ConfigurationConverged", "The Talos machine configuration was applied and the Node recovered.")
		if release != nil {
			release()
		}
	} else if outcome.FailureMessage != "" {
		r.setConfigurationCondition(machine, metav1.ConditionFalse, "ConfigurationUpdateFailed", outcome.FailureMessage)
		if release != nil {
			release()
		}
	} else {
		r.setConfigurationCondition(machine, metav1.ConditionFalse, "ConfigurationUpdatePending", outcome.RetryMessage)
	}
	return outcome
}

func desiredEndpoint(configuration []byte) string {
	endpoint, err := configbuilder.ControlPlaneEndpoint(configuration)
	if err != nil {
		return ""
	}
	return endpoint
}

func (r *TartMachineReconciler) configurationUpdateFailure(machine *infrav1alpha1.TartMachine, reason, message string) runtimeextension.ConfigurationUpdateOutcome {
	r.setConfigurationCondition(machine, metav1.ConditionFalse, reason, message)
	return runtimeextension.ConfigurationUpdateOutcome{FailureMessage: message}
}

func setConfigurationCondition(machine *infrav1alpha1.TartMachine, status metav1.ConditionStatus, reason, message string) {
	controller.SetCondition(&machine.Status.Conditions, infrav1alpha1.TartMachineConfigurationUpToDateCondition, status, reason, message, machine.Generation)
}

func (r *TartMachineReconciler) setConfigurationCondition(machine *infrav1alpha1.TartMachine, status metav1.ConditionStatus, reason, message string) {
	setConfigurationCondition(machine, status, reason, message)
}

func (r *TartMachineReconciler) bootstrapConfigForMachine(ctx context.Context, machine *infrav1alpha1.TartMachine) (*clusterv1.Machine, *bootstrapv1alpha1.TartBootstrapConfig, error) {
	capiMachine, err := controller.FindCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if err != nil {
		return nil, nil, err
	}
	ref := capiMachine.Spec.Bootstrap.ConfigRef
	if ref.APIGroup != bootstrapv1alpha1.GroupVersion.Group || ref.Kind != controller.TartBootstrapConfigKind || ref.Name == "" {
		return nil, nil, ErrBootstrapDataUnavailable
	}
	config := &bootstrapv1alpha1.TartBootstrapConfig{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: capiMachine.Namespace, Name: ref.Name}, config); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil, ErrBootstrapDataUnavailable
		}
		return nil, nil, err
	}
	return capiMachine, config, nil
}

func (r *TartMachineReconciler) endpointUpdateGate(ctx context.Context, machine *clusterv1.Machine, desiredEndpoint string) (bool, string) {
	if machine == nil || machine.Spec.ClusterName == "" || desiredEndpoint == "" {
		return false, "The control-plane endpoint update context is incomplete; waiting for observed cluster state."
	}
	var machines clusterv1.MachineList
	if err := r.List(ctx, &machines, client.InNamespace(machine.Namespace), client.MatchingLabels{clusterv1.ClusterNameLabel: machine.Spec.ClusterName}); err != nil {
		return false, "The cluster Machine inventory could not be observed before the control-plane endpoint update."
	}
	controlPlanes := make([]clusterv1.Machine, 0, len(machines.Items))
	for index := range machines.Items {
		candidate := machines.Items[index]
		if candidate.Spec.ClusterName != machine.Spec.ClusterName || candidate.DeletionTimestamp != nil {
			continue
		}
		if _, ok := candidate.Labels[clusterv1.MachineControlPlaneLabel]; ok {
			controlPlanes = append(controlPlanes, candidate)
		}
	}
	if len(controlPlanes) == 0 {
		return false, "No control-plane Machines are available for the endpoint update."
	}
	slices.SortFunc(controlPlanes, func(left, right clusterv1.Machine) int { return cmp.Compare(left.Name, right.Name) })
	if isControlPlaneMachineForEndpoint(machine) {
		targetFound := false
		for index := range controlPlanes {
			candidate := &controlPlanes[index]
			if candidate.UID == machine.UID {
				targetFound = true
				break
			}
			if !r.machineEndpointConverged(ctx, candidate, desiredEndpoint) {
				return false, "A preceding control-plane Machine has not recovered on the new control-plane endpoint."
			}
		}
		if !targetFound {
			return false, "The target control-plane Machine is not present in the observed cluster inventory."
		}
	} else {
		for index := range controlPlanes {
			if !r.machineEndpointConverged(ctx, &controlPlanes[index], desiredEndpoint) {
				return false, "All control-plane Machines must converge on the new control-plane endpoint before a worker update."
			}
		}
	}
	return true, ""
}

func isControlPlaneMachineForEndpoint(machine *clusterv1.Machine) bool {
	if machine == nil {
		return false
	}
	_, ok := machine.Labels[clusterv1.MachineControlPlaneLabel]
	return ok
}

func (r *TartMachineReconciler) machineEndpointConverged(ctx context.Context, machine *clusterv1.Machine, endpoint string) bool {
	if !isControlPlaneMachineForEndpoint(machine) {
		return false
	}
	if ready := meta.FindStatusCondition(machine.Status.Conditions, clusterv1.MachineReadyCondition); ready == nil || ready.Status != metav1.ConditionTrue {
		return false
	}
	ref := machine.Spec.InfrastructureRef
	if ref.APIGroup != infrav1alpha1.GroupVersion.Group || ref.Kind != controller.TartMachineKind || ref.Name == "" {
		return false
	}
	provider := &infrav1alpha1.TartMachine{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: ref.Name}, provider); err != nil {
		return false
	}
	condition := meta.FindStatusCondition(provider.Status.Conditions, infrav1alpha1.TartMachineConfigurationUpToDateCondition)
	return provider.Status.ObservedControlPlaneEndpoint == endpoint && condition != nil && condition.Status == metav1.ConditionTrue
}

func (r *TartMachineReconciler) acquireEndpointLease(ctx context.Context, machine *clusterv1.Machine) (bool, error) {
	if machine == nil || machine.UID == "" || machine.Spec.ClusterName == "" {
		return false, fmt.Errorf("control-plane endpoint lease identity is incomplete")
	}
	name := endpointLeaseName(machine.Spec.ClusterName)
	now := metav1.NowMicro()
	lease := &coordinationv1.Lease{}
	err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: name}, lease)
	if apierrors.IsNotFound(err) {
		lease = &coordinationv1.Lease{
			Name: name, Namespace: machine.Namespace,
			Spec: coordinationv1.LeaseSpec{HolderIdentity: new(string(machine.UID)), LeaseDurationSeconds: new(endpointLeaseDuration), RenewTime: &now},
		}
		if err := r.Create(ctx, lease); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity != "" && *lease.Spec.HolderIdentity != string(machine.UID) && !leaseExpired(lease, now.Time) {
		return false, nil
	}
	original := lease.DeepCopy()
	lease.Spec.HolderIdentity = new(string(machine.UID))
	lease.Spec.LeaseDurationSeconds = new(endpointLeaseDuration)
	lease.Spec.RenewTime = &now
	if err := r.Patch(ctx, lease, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		if apierrors.IsConflict(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *TartMachineReconciler) releaseEndpointLease(ctx context.Context, machine *clusterv1.Machine) error {
	if machine == nil || machine.UID == "" {
		return nil
	}
	lease := &coordinationv1.Lease{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: endpointLeaseName(machine.Spec.ClusterName)}, lease); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity != string(machine.UID) {
		return nil
	}
	original := lease.DeepCopy()
	lease.Spec.HolderIdentity = nil
	lease.Spec.RenewTime = nil
	return r.Patch(ctx, lease, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{}))
}

func endpointLeaseName(clusterName string) string {
	hash := sha256.Sum256([]byte(clusterName))
	return endpointLeasePrefix + hex.EncodeToString(hash[:])[:16]
}

func leaseExpired(lease *coordinationv1.Lease, now time.Time) bool {
	if lease == nil || lease.Spec.RenewTime == nil || lease.Spec.LeaseDurationSeconds == nil {
		return true
	}
	return lease.Spec.RenewTime.Add(time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second).Before(now)
}
