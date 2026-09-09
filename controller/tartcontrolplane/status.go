package tartcontrolplane

import (
	"slices"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controller"
)

func setControlPlaneStatus(cp *controlplanev1alpha1.TartControlPlane, clusterName string, desired int32, machines []clusterv1.Machine, bootstrapState controlPlaneBootstrapState, caRotationState controlPlaneCARotationState, upgradeState controlPlaneKubernetesUpgradeState) {
	actual := int32(len(machines))
	ready := countMachineCondition(machines, clusterv1.MachineReadyCondition)
	available := countMachineCondition(machines, clusterv1.MachineAvailableCondition)
	upToDate := countMachineCondition(machines, clusterv1.MachineUpToDateCondition)
	cp.Status.Selector = labels.SelectorFromSet(labels.Set{
		clusterv1.ClusterNameLabel:             clusterName,
		clusterv1.MachineControlPlaneNameLabel: cp.Name,
	}).String()
	cp.Status.Replicas = new(actual)
	cp.Status.ReadyReplicas = new(ready)
	cp.Status.AvailableReplicas = new(available)
	cp.Status.UpToDateReplicas = new(upToDate)
	cp.Status.Versions = machineVersions(machines)
	if bootstrapState.initialized {
		cp.Status.Initialization.ControlPlaneInitialized = new(true)
	} else if cp.Status.Initialization.ControlPlaneInitialized == nil {
		cp.Status.Initialization.ControlPlaneInitialized = new(false)
	}

	controlPlaneReady := bootstrapState.workloadReady && desired > 0 && ready == desired
	availableReason := "MachinesNotAvailable"
	availableMessage := "Not all desired control-plane Machines are available."
	if !bootstrapState.workloadReady {
		availableReason = controller.ReasonWorkloadAPIUnavailable
		availableMessage = "The workload Kubernetes API is not available yet."
	} else if controlPlaneReady {
		availableReason = "Available"
		availableMessage = "All desired control-plane Machines and the workload Kubernetes API are available."
	}
	controller.SetCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneAvailableCondition, conditionStatus(controlPlaneReady), availableReason, availableMessage, cp.Generation)
	// Kubernetes version upgradeが未収束の間はUpToDateへ倒さない。desired versionのsource of truthはspec.versionである。
	// upgradeState.unknownの間は、staleなobservedVersionがdesiredと偶然一致していてもUpToDateへ断定しない。
	kubernetesUpToDate := !upgradeState.unknown && talos.NormalizeKubernetesVersion(upgradeState.observedVersion) == talos.NormalizeKubernetesVersion(cp.Spec.Version)
	upToDateReady := controlPlaneReady && desired > 0 && upToDate == desired && kubernetesUpToDate && !upgradeState.active
	upToDateReason := "MachinesNotUpToDate"
	upToDateMessage := "Not all desired control-plane Machines report UpToDate."
	if !bootstrapState.workloadReady {
		upToDateReason = controller.ReasonWorkloadAPIUnavailable
		upToDateMessage = "The workload Kubernetes API is not ready yet."
	} else if !kubernetesUpToDate || upgradeState.active {
		upToDateReason = "KubernetesVersionNotUpToDate"
		upToDateMessage = "The cluster has not converged to the desired Kubernetes version yet."
	} else if upToDateReady {
		upToDateReason = "UpToDate"
		upToDateMessage = "All desired control-plane Machines and the workload Kubernetes API are up to date."
	}
	controller.SetCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneUpToDateCondition, conditionStatus(upToDateReady), upToDateReason, upToDateMessage, cp.Generation)
	controller.SetCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneRollingOutCondition, metav1.ConditionFalse, "NotRollingOut", "The control plane is not performing a rollout.", cp.Generation)
	controller.SetCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneScalingUpCondition, conditionStatus(actual < desired), scalingReason(actual < desired, "ScalingUp"), scalingMessage(actual < desired, "Control-plane Machines are being created.", "The desired control-plane replica count is satisfied."), cp.Generation)
	controller.SetCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneScalingDownCondition, conditionStatus(actual > desired), scalingReason(actual > desired, "ScalingDown"), scalingMessage(actual > desired, "Control-plane scale-down is waiting for quorum-safe etcd member removal.", "The desired control-plane replica count is not above the observed count."), cp.Generation)
	controller.SetCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneMachinesReadyCondition, conditionStatus(desired > 0 && ready == desired), machineReadinessReason(desired > 0 && ready == desired), machineReadinessMessage(desired > 0 && ready == desired), cp.Generation)
	controller.SetCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneMachinesUpToDateCondition, conditionStatus(desired > 0 && upToDate == desired), machineUpToDateReason(desired > 0 && upToDate == desired), machineUpToDateMessage(desired > 0 && upToDate == desired), cp.Generation)
	controller.SetCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneEtcdClusterAvailableCondition, conditionStatus(bootstrapState.etcdReady), bootstrapState.reason, bootstrapState.message, cp.Generation)
	controller.SetCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneCARotatingCondition, conditionStatus(caRotationState.active), caRotationState.reason, caRotationState.message, cp.Generation)
	cp.Status.KubernetesUpgrade = controlplanev1alpha1.TartControlPlaneKubernetesUpgradeStatus{
		TargetVersion:   upgradeState.targetVersion,
		ObservedVersion: upgradeState.observedVersion,
	}
	kubernetesUpgradingStatus := conditionStatus(upgradeState.active)
	if upgradeState.unknown {
		kubernetesUpgradingStatus = metav1.ConditionUnknown
	}
	controller.SetCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneKubernetesUpgradingCondition, kubernetesUpgradingStatus, upgradeState.reason, upgradeState.message, cp.Generation)
	controller.SetCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneDeletingCondition, metav1.ConditionFalse, "NotDeleting", "The control plane is not being deleted.", cp.Generation)
	controller.SetPausedCondition(&cp.Status.Conditions, false, cp.Generation)
	cp.Status.ObservedGeneration = cp.Generation
}

func countMachineCondition(machines []clusterv1.Machine, conditionType string) int32 {
	var count int32
	for i := range machines {
		condition := meta.FindStatusCondition(machines[i].Status.Conditions, conditionType)
		if condition != nil && condition.Status == metav1.ConditionTrue {
			count++
		}
	}
	return count
}

func machineVersions(machines []clusterv1.Machine) []clusterv1.StatusVersion {
	counts := make(map[string]int32)
	for i := range machines {
		if machines[i].Status.NodeInfo != nil && machines[i].Status.NodeInfo.KubeletVersion != "" {
			counts[machines[i].Status.NodeInfo.KubeletVersion]++
		}
	}
	versions := make([]string, 0, len(counts))
	for version := range counts {
		versions = append(versions, version)
	}
	slices.Sort(versions)
	result := make([]clusterv1.StatusVersion, 0, len(versions))
	for _, version := range versions {
		result = append(result, clusterv1.StatusVersion{Version: version, Replicas: counts[version]})
	}
	return result
}

func conditionStatus(value bool) metav1.ConditionStatus {
	if value {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

func scalingReason(value bool, activeReason string) string {
	if value {
		return activeReason
	}
	return "NotScaling"
}

func scalingMessage(value bool, activeMessage, inactiveMessage string) string {
	if value {
		return activeMessage
	}
	return inactiveMessage
}

func machineReadinessReason(ready bool) string {
	if ready {
		return "MachinesReady"
	}
	return "MachinesNotReady"
}

func machineReadinessMessage(ready bool) string {
	if ready {
		return "All desired control-plane Machines report Ready."
	}
	return "Not all desired control-plane Machines report Ready."
}

func machineUpToDateReason(upToDate bool) string {
	if upToDate {
		return "MachinesUpToDate"
	}
	return "MachinesNotUpToDate"
}

func machineUpToDateMessage(upToDate bool) string {
	if upToDate {
		return "All desired control-plane Machines report UpToDate."
	}
	return "Not all desired control-plane Machines report UpToDate."
}
