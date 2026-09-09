package tartcontrolplane

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controller"
)

func (r *TartControlPlaneReconciler) getTartMachineTemplate(ctx context.Context, namespace string, ref *clusterv1.ContractVersionedObjectReference, template *infrav1alpha1.TartMachineTemplate) error {
	if ref.APIGroup != infrav1alpha1.GroupVersion.Group || ref.Kind != "TartMachineTemplate" || ref.Name == "" {
		return &controlPlaneFailure{
			reason:  "MachineTemplateInvalid",
			message: "The control-plane infrastructureRef must reference a TartMachineTemplate.",
		}
	}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, template); err != nil {
		if apierrors.IsNotFound(err) {
			return &controlPlaneFailure{
				reason:  "MachineTemplateUnavailable",
				message: "The referenced TartMachineTemplate is not available yet.",
			}
		}
		return err
	}
	return nil
}

func (r *TartControlPlaneReconciler) getBootstrapTemplate(ctx context.Context, namespace string, ref *clusterv1.ContractVersionedObjectReference, template *bootstrapv1alpha1.TartBootstrapConfigTemplate) error {
	if ref.APIGroup != bootstrapv1alpha1.GroupVersion.Group || ref.Kind != "TartBootstrapConfigTemplate" || ref.Name == "" {
		return &controlPlaneFailure{
			reason:  controller.ReasonBootstrapTemplateInvalid,
			message: "The bootstrapConfigTemplate must reference a TartBootstrapConfigTemplate.",
		}
	}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, template); err != nil {
		if apierrors.IsNotFound(err) {
			return &controlPlaneFailure{
				reason:  "BootstrapTemplateUnavailable",
				message: "The referenced TartBootstrapConfigTemplate is not available yet.",
			}
		}
		return err
	}
	return nil
}

func (r *TartControlPlaneReconciler) ensureMachines(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, clusterName string, desired int32, failureDomains []clusterv1.FailureDomain, machineTemplate *infrav1alpha1.TartMachineTemplate, bootstrapTemplate *bootstrapv1alpha1.TartBootstrapConfigTemplate) ([]clusterv1.Machine, error) {
	var list clusterv1.MachineList
	if err := r.List(ctx, &list, client.InNamespace(cp.Namespace), client.MatchingLabels{
		clusterv1.ClusterNameLabel:             clusterName,
		clusterv1.MachineControlPlaneNameLabel: cp.Name,
	}); err != nil {
		return nil, err
	}
	if err := validateControlPlaneMachineOwners(list.Items, cp); err != nil {
		return nil, err
	}
	byName := make(map[string]*clusterv1.Machine, len(list.Items))
	for i := range list.Items {
		byName[list.Items[i].Name] = &list.Items[i]
	}

	for ordinal := range int(desired) {
		ordinal32 := int32(ordinal)
		failureDomain, failureDomainOK := controlPlaneFailureDomain(failureDomains, ordinal)
		if len(failureDomains) > 0 && !failureDomainOK {
			return nil, &controlPlaneFailure{reason: "FailureDomainInvalid", message: "The TartCluster exposes no Failure Domain suitable for control-plane Machines."}
		}
		machineName, err := controlPlaneChildName(cp.Name, ordinal32, "")
		if err != nil {
			return nil, &controlPlaneFailure{reason: controller.ReasonMachineNameInvalid, message: "A deterministic control-plane Machine name is invalid."}
		}
		machine, ok := byName[machineName]
		if !ok {
			machine, err = r.createMachine(ctx, cp, clusterName, ordinal32, failureDomain, machineName)
			if err != nil {
				return nil, err
			}
			byName[machine.Name] = machine
		}
		bootstrapName, nameErr := bootstrapConfigName(cp.Name, ordinal32)
		if nameErr != nil {
			return nil, &controlPlaneFailure{reason: controller.ReasonMachineNameInvalid, message: controller.BootstrapConfigNameInvalidMessage}
		}
		if err := validateMachineReference(machine, cp, clusterName, machineName, bootstrapName); err != nil {
			return nil, err
		}
		if machine.Spec.FailureDomain != "" && !containsControlPlaneFailureDomain(failureDomains, machine.Spec.FailureDomain) {
			return nil, &controlPlaneFailure{reason: "FailureDomainInvalid", message: "An existing control-plane Machine references a Failure Domain no longer exposed by TartCluster."}
		}
		if err := r.ensureMachineTemplateFields(ctx, machine, cp); err != nil {
			return nil, err
		}
		if err := r.ensureProviderResources(ctx, cp, clusterName, ordinal32, machine, machineTemplate, bootstrapTemplate); err != nil {
			return nil, err
		}
	}

	var refreshed clusterv1.MachineList
	if err := r.List(ctx, &refreshed, client.InNamespace(cp.Namespace), client.MatchingLabels{
		clusterv1.ClusterNameLabel:             clusterName,
		clusterv1.MachineControlPlaneNameLabel: cp.Name,
	}); err != nil {
		return nil, err
	}
	if err := validateControlPlaneMachineOwners(refreshed.Items, cp); err != nil {
		return nil, err
	}
	if int32(len(refreshed.Items)) > desired {
		pending, err := r.reconcileScaleDown(ctx, refreshed.Items, desired)
		if err != nil {
			return refreshed.Items, err
		}
		if pending {
			return refreshed.Items, errControlPlaneScaleDownPending
		}
	}
	return refreshed.Items, nil
}

func controlPlaneFailureDomain(failureDomains []clusterv1.FailureDomain, ordinal int) (string, bool) {
	eligible := make([]string, 0, len(failureDomains))
	for index := range failureDomains {
		if failureDomains[index].Name == "" || (failureDomains[index].ControlPlane != nil && !*failureDomains[index].ControlPlane) {
			continue
		}
		if !slices.Contains(eligible, failureDomains[index].Name) {
			eligible = append(eligible, failureDomains[index].Name)
		}
	}
	if len(eligible) == 0 {
		return "", false
	}
	slices.Sort(eligible)
	return eligible[ordinal%len(eligible)], true
}

func containsControlPlaneFailureDomain(failureDomains []clusterv1.FailureDomain, name string) bool {
	return slices.ContainsFunc(failureDomains, func(domain clusterv1.FailureDomain) bool {
		return domain.Name == name && (domain.ControlPlane == nil || *domain.ControlPlane)
	})
}

func validateControlPlaneMachineOwners(machines []clusterv1.Machine, cp *controlplanev1alpha1.TartControlPlane) error {
	for index := range machines {
		if !controller.HasControllerOwner(&machines[index], cp, controlplanev1alpha1.GroupVersion.String(), controller.TartControlPlaneKind) {
			return &controlPlaneFailure{
				reason:  "MachineOwnershipMismatch",
				message: "A labeled control-plane Machine is not owned by this TartControlPlane; scale-down is stopped.",
			}
		}
	}
	return nil
}

func (r *TartControlPlaneReconciler) ensureMachineTemplateFields(ctx context.Context, machine *clusterv1.Machine, cp *controlplanev1alpha1.TartControlPlane) error {
	if !machine.DeletionTimestamp.IsZero() {
		return nil
	}
	expectedReadinessGates := slices.Clone(cp.Spec.MachineTemplate.Spec.ReadinessGates)
	expectedTaints := slices.Clone(cp.Spec.MachineTemplate.Spec.Taints)
	expectedDeletion := machineDeletionSpec(cp.Spec.MachineTemplate.Spec.Deletion)
	// Spec.Versionはcluster-wide Kubernetes upgradeでreconcileKubernetesUpgradeがin-placeに
	// desired versionへ収束させるmutable fieldである。ここで同期しない場合、cp.Spec.Versionを
	// 変更してもMachine.Spec.Versionが古いままとなり、validateMachineReferenceとは別の
	// 場所で不整合が残り続ける。
	if reflect.DeepEqual(machine.Spec.ReadinessGates, expectedReadinessGates) && reflect.DeepEqual(machine.Spec.Taints, expectedTaints) && reflect.DeepEqual(machine.Spec.Deletion, expectedDeletion) && machine.Spec.Version == cp.Spec.Version {
		return nil
	}
	original := machine.DeepCopy()
	machine.Spec.ReadinessGates = expectedReadinessGates
	machine.Spec.Taints = expectedTaints
	machine.Spec.Deletion = expectedDeletion
	machine.Spec.Version = cp.Spec.Version
	return r.Patch(ctx, machine, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{}))
}

func machineDeletionSpec(spec controlplanev1alpha1.TartControlPlaneMachineTemplateDeletionSpec) clusterv1.MachineDeletionSpec {
	nodeDeletionTimeout := cloneInt32(spec.NodeDeletionTimeoutSeconds)
	if nodeDeletionTimeout == nil {
		nodeDeletionTimeout = new(capiDefaultNodeDeletionTimeoutSeconds)
	}
	return clusterv1.MachineDeletionSpec{
		NodeDrainTimeoutSeconds:        cloneInt32(spec.NodeDrainTimeoutSeconds),
		NodeVolumeDetachTimeoutSeconds: cloneInt32(spec.NodeVolumeDetachTimeoutSeconds),
		NodeDeletionTimeoutSeconds:     nodeDeletionTimeout,
	}
}

func cloneInt32(value *int32) *int32 {
	if value == nil {
		return nil
	}
	return new(*value)
}

// reconcileScaleDownは一度に一台だけを選び、etcd member removalが観測できるまでCAPI Machineのpre-terminate hookを保持する。

func (r *TartControlPlaneReconciler) createMachine(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, clusterName string, ordinal int32, failureDomain, name string) (*clusterv1.Machine, error) {
	bootstrapName, err := bootstrapConfigName(cp.Name, ordinal)
	if err != nil {
		return nil, &controlPlaneFailure{reason: controller.ReasonMachineNameInvalid, message: controller.BootstrapConfigNameInvalidMessage}
	}
	objectLabels, annotations := controlPlaneMetadata(cp.Spec.MachineTemplate.ObjectMeta, clusterName, cp.Name, ordinal)
	machine := &clusterv1.Machine{
		APIVersion:      clusterv1.GroupVersion.String(),
		Kind:            controller.CAPIMachineKind,
		Name:            name,
		Namespace:       cp.Namespace,
		Labels:          objectLabels,
		Annotations:     annotations,
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(cp, controlplanev1alpha1.GroupVersion.String(), controller.TartControlPlaneKind)},
		Spec: clusterv1.MachineSpec{
			ClusterName:    clusterName,
			Version:        cp.Spec.Version,
			FailureDomain:  failureDomain,
			ReadinessGates: slices.Clone(cp.Spec.MachineTemplate.Spec.ReadinessGates),
			Taints:         slices.Clone(cp.Spec.MachineTemplate.Spec.Taints),
			Deletion:       machineDeletionSpec(cp.Spec.MachineTemplate.Spec.Deletion),
			Bootstrap: clusterv1.Bootstrap{ConfigRef: clusterv1.ContractVersionedObjectReference{
				APIGroup: bootstrapv1alpha1.GroupVersion.Group,
				Kind:     controller.TartBootstrapConfigKind,
				Name:     bootstrapName,
			}},
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				APIGroup: infrav1alpha1.GroupVersion.Group,
				Kind:     controller.TartMachineKind,
				Name:     name,
			},
		},
	}
	if err := r.Create(ctx, machine); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, err
	}
	var current clusterv1.Machine
	if err := r.Get(ctx, client.ObjectKey{Namespace: cp.Namespace, Name: name}, &current); err != nil {
		return nil, err
	}
	return &current, nil
}

func (r *TartControlPlaneReconciler) ensureProviderResources(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, clusterName string, ordinal int32, machine *clusterv1.Machine, machineTemplate *infrav1alpha1.TartMachineTemplate, bootstrapTemplate *bootstrapv1alpha1.TartBootstrapConfigTemplate) error {
	if machine.UID == "" {
		return &controlPlaneFailure{reason: "MachineIdentityUnavailable", message: "The CAPI Machine has no UID yet; provider resources are not created."}
	}
	machineName := machine.Name
	providerLabels, _ := controlPlaneMetadata(machineTemplate.Spec.Template.ObjectMeta, clusterName, cp.Name, ordinal)
	expectedMachine := &infrav1alpha1.TartMachine{
		APIVersion:      infrav1alpha1.GroupVersion.String(),
		Kind:            controller.TartMachineKind,
		Name:            machineName,
		Namespace:       cp.Namespace,
		Labels:          providerLabels,
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(machine, clusterv1.GroupVersion.String(), controller.CAPIMachineKind)},
		Spec: infrav1alpha1.TartMachineSpec{
			HostSelector: machineTemplate.Spec.Template.Spec.HostSelector.DeepCopy(),
			Image:        machineTemplate.Spec.Template.Spec.Image,
		},
	}
	var tartMachine infrav1alpha1.TartMachine
	if err := r.Get(ctx, client.ObjectKey{Namespace: cp.Namespace, Name: machineName}, &tartMachine); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		if err := r.Create(ctx, expectedMachine); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
		if err := r.Get(ctx, client.ObjectKey{Namespace: cp.Namespace, Name: machineName}, &tartMachine); err != nil {
			return err
		}
	}
	if err := controller.ValidateProviderOwner(&tartMachine, machine, clusterv1.GroupVersion.String(), controller.CAPIMachineKind); err != nil {
		return err
	}
	if !reflect.DeepEqual(tartMachine.Spec.HostSelector, expectedMachine.Spec.HostSelector) || tartMachine.Spec.Image != expectedMachine.Spec.Image {
		return &controlPlaneFailure{reason: controller.ReasonMachineSpecMismatch, message: "The existing TartMachine does not match its immutable control-plane template."}
	}

	bootstrapName, err := bootstrapConfigName(cp.Name, ordinal)
	if err != nil {
		return &controlPlaneFailure{reason: controller.ReasonMachineNameInvalid, message: controller.BootstrapConfigNameInvalidMessage}
	}
	bootstrapLabels, _ := controlPlaneMetadata(bootstrapTemplate.Spec.Template.ObjectMeta, clusterName, cp.Name, ordinal)
	expectedBootstrap := &bootstrapv1alpha1.TartBootstrapConfig{
		APIVersion:      bootstrapv1alpha1.GroupVersion.String(),
		Kind:            controller.TartBootstrapConfigKind,
		Name:            bootstrapName,
		Namespace:       cp.Namespace,
		Labels:          bootstrapLabels,
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(machine, clusterv1.GroupVersion.String(), controller.CAPIMachineKind)},
		Spec: bootstrapv1alpha1.TartBootstrapConfigSpec{
			ConfigPatchesSecretRef: bootstrapTemplate.Spec.Template.Spec.ConfigPatchesSecretRef.DeepCopy(),
			UpdatePolicy:           bootstrapTemplate.Spec.Template.Spec.UpdatePolicy,
		},
	}
	var bootstrapConfig bootstrapv1alpha1.TartBootstrapConfig
	if err := r.Get(ctx, client.ObjectKey{Namespace: cp.Namespace, Name: bootstrapName}, &bootstrapConfig); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		if err := r.Create(ctx, expectedBootstrap); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
		if err := r.Get(ctx, client.ObjectKey{Namespace: cp.Namespace, Name: bootstrapName}, &bootstrapConfig); err != nil {
			return err
		}
	}
	if err := controller.ValidateProviderOwner(&bootstrapConfig, machine, clusterv1.GroupVersion.String(), controller.CAPIMachineKind); err != nil {
		return err
	}
	actualRef := bootstrapConfig.Spec.ConfigPatchesSecretRef
	expectedRef := expectedBootstrap.Spec.ConfigPatchesSecretRef
	if (actualRef == nil) != (expectedRef == nil) || (actualRef != nil && actualRef.Name != expectedRef.Name) {
		return &controlPlaneFailure{reason: "BootstrapConfigMismatch", message: "The existing TartBootstrapConfig does not match its immutable template."}
	}
	return nil
}

// validateMachineReferenceは、既存Machineのimmutableな参照fieldがTartControlPlaneの期待と
// 一致しているかを検証する。Spec.Versionはcluster-wide Kubernetes upgradeでin-placeに変化する
// mutable fieldであるため、ここでは検証しない(同期はensureMachineTemplateFieldsが担う)。
func validateMachineReference(machine *clusterv1.Machine, cp *controlplanev1alpha1.TartControlPlane, clusterName, machineName, bootstrapName string) error {
	if !controller.HasControllerOwner(machine, cp, controlplanev1alpha1.GroupVersion.String(), controller.TartControlPlaneKind) || machine.Spec.ClusterName != clusterName || machine.Spec.InfrastructureRef.APIGroup != infrav1alpha1.GroupVersion.Group || machine.Spec.InfrastructureRef.Kind != controller.TartMachineKind || machine.Spec.InfrastructureRef.Name != machineName || machine.Spec.Bootstrap.ConfigRef.APIGroup != bootstrapv1alpha1.GroupVersion.Group || machine.Spec.Bootstrap.ConfigRef.Kind != controller.TartBootstrapConfigKind || machine.Spec.Bootstrap.ConfigRef.Name != bootstrapName {
		return &controlPlaneFailure{reason: controller.ReasonMachineSpecMismatch, message: "The existing control-plane Machine does not match the TartControlPlane references."}
	}
	return nil
}

func controllerOwnerReference(owner metav1.Object, apiVersion, kind string) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         apiVersion,
		Kind:               kind,
		Name:               owner.GetName(),
		UID:                owner.GetUID(),
		Controller:         new(true),
		BlockOwnerDeletion: new(true),
	}
}

func controlPlaneMetadata(template clusterv1.ObjectMeta, clusterName, controlPlaneName string, ordinal int32) (map[string]string, map[string]string) {
	objectLabels := maps.Clone(template.Labels)
	if objectLabels == nil {
		objectLabels = make(map[string]string)
	}
	objectLabels[clusterv1.ClusterNameLabel] = clusterName
	objectLabels[clusterv1.MachineControlPlaneLabel] = ""
	objectLabels[clusterv1.MachineControlPlaneNameLabel] = controlPlaneName
	objectLabels[controlPlaneOrdinalLabel] = strconv.FormatInt(int64(ordinal), 10)

	annotations := maps.Clone(template.Annotations)
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[clusterv1.MachineSkipRemediationAnnotation] = "true"
	return objectLabels, annotations
}

func controlPlaneChildName(controlPlaneName string, ordinal int32, suffix string) (string, error) {
	name := controlPlaneName + "-" + strconv.FormatInt(int64(ordinal), 10)
	if suffix != "" {
		name += "-" + suffix
	}
	if len(validation.IsDNS1123Subdomain(name)) != 0 {
		return "", fmt.Errorf("invalid control-plane child name")
	}
	return name, nil
}

func bootstrapConfigName(controlPlaneName string, ordinal int32) (string, error) {
	return controlPlaneChildName(controlPlaneName, ordinal, "bootstrap")
}
