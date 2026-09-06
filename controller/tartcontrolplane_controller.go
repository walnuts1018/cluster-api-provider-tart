package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controlplane"
	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
	"github.com/walnuts1018/cluster-api-provider-tart/talos"
)

const (
	controlPlaneOrdinalLabel     = "tart.cluster.x-k8s.io/control-plane-index"
	controlPlaneEtcdMemberID     = "tart.cluster.x-k8s.io/etcd-member-id"
	controlPlaneEtcdDeleteHook   = clusterv1.PreTerminateDeleteHookAnnotationPrefix + "/tart-etcd-member"
	controlPlaneScaleDownRequeue = 30 * time.Second
	// reasonCATrustUpdateFailedはCA rotationの各段階でTalos machine configurationへのapplyが失敗した場合のreasonである。
	reasonCATrustUpdateFailed = "CATrustUpdateFailed"
	// reasonCAConfigurationUnrecognizedは、observationのCA trust stageが既知のいずれの段階とも一致しない場合のreasonである。
	reasonCAConfigurationUnrecognized = "CAConfigurationUnrecognized"
	// messageCAConfigurationUnrecognizedはreasonCAConfigurationUnrecognizedに対応するmessageである。
	messageCAConfigurationUnrecognized = "A control-plane Machine reports a CA trust configuration that cannot be safely classified; CA rotation is stopped."
)

// TartControlPlaneReconcilerはTartControlPlane objectをreconcileする。
type TartControlPlaneReconciler struct {
	client.Client

	// KubernetesUpgradeはcluster-wide Kubernetes upgradeの実行者である。nilの場合はTalos upstream実装を使う。
	KubernetesUpgrade talos.KubernetesUpgradeRunner
	// KubernetesUpgradeIdentityはupgrade leaseのholder identityである。nilの場合はhost名とprocess IDから導出する。
	KubernetesUpgradeIdentity string
}

type controlPlaneFailure struct {
	reason  string
	message string
}

type controlPlaneBootstrapState struct {
	initialized   bool
	etcdReady     bool
	workloadReady bool
	reason        string
	message       string
	requeueAfter  time.Duration
}

var errControlPlaneScaleDownPending = errors.New("control-plane scale-down is waiting for etcd member removal")

func (f *controlPlaneFailure) Error() string {
	return f.reason + ": " + f.message
}

// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=tartcontrolplanes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=tartcontrolplanes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachinetemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs,verbs=get;list;watch;create
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update

func (r *TartControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cp controlplanev1alpha1.TartControlPlane
	if err := r.Get(ctx, req.NamespacedName, &cp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if isPaused(&cp) {
		return r.reconcilePaused(ctx, &cp)
	}
	if !cp.DeletionTimestamp.IsZero() {
		return r.reconcileDeleting(ctx, &cp)
	}

	clusterName, err := controlPlaneClusterName(&cp)
	if err != nil {
		return r.reportFailure(ctx, &cp, err)
	}
	if cp.Spec.Version == "" {
		return r.reportFailure(ctx, &cp, &controlPlaneFailure{
			reason:  "InvalidSpec",
			message: "The Kubernetes version is required before control-plane Machines can be created.",
		})
	}
	desiredReplicas, err := desiredControlPlaneReplicas(&cp)
	if err != nil {
		return r.reportFailure(ctx, &cp, err)
	}

	var cluster clusterv1.Cluster
	if err := r.Get(ctx, client.ObjectKey{Namespace: cp.Namespace, Name: clusterName}, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return r.reportFailure(ctx, &cp, &controlPlaneFailure{
				reason:  reasonClusterUnavailable,
				message: "The referenced CAPI Cluster is not available yet.",
			})
		}
		return ctrl.Result{}, err
	}

	tartCluster, err := r.getTartCluster(ctx, &cluster)
	if err != nil {
		return r.reportFailure(ctx, &cp, err)
	}
	if err := r.validateActiveBundle(ctx, tartCluster); err != nil {
		return r.reportFailure(ctx, &cp, err)
	}

	var machineTemplate infrav1alpha1.TartMachineTemplate
	if err := r.getTartMachineTemplate(ctx, cp.Namespace, &cp.Spec.MachineTemplate.Spec.InfrastructureRef, &machineTemplate); err != nil {
		return r.reportFailure(ctx, &cp, err)
	}
	var bootstrapTemplate bootstrapv1alpha1.TartBootstrapConfigTemplate
	if err := r.getBootstrapTemplate(ctx, cp.Namespace, &cp.Spec.BootstrapConfigTemplateRef, &bootstrapTemplate); err != nil {
		return r.reportFailure(ctx, &cp, err)
	}
	machines, err := r.ensureMachines(ctx, &cp, clusterName, desiredReplicas, tartCluster.Status.FailureDomains, &machineTemplate, &bootstrapTemplate)
	scaleDownPending := errors.Is(err, errControlPlaneScaleDownPending)
	if err != nil && !scaleDownPending {
		if failure, ok := errors.AsType[*controlPlaneFailure](err); ok {
			return r.reportFailure(ctx, &cp, failure)
		}
		return ctrl.Result{}, err
	}
	bootstrapState, err := r.reconcileControlPlaneBootstrap(ctx, &cp, &cluster, machines)
	if err != nil {
		return ctrl.Result{}, err
	}
	caRotationState, err := r.reconcileCARotation(ctx, tartCluster, machines, scaleDownPending)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Kubernetes version upgradeはcluster-wide operationであり、TartControlPlaneだけが所有する。
	// CA rotationやscale-downと同時には開始せず、他のlifecycle operationの完了を待つ。
	upgradeState := r.reconcileKubernetesUpgrade(ctx, &cp, &cluster, machines, bootstrapState, caRotationState.active || scaleDownPending, desiredReplicas)

	original := cp.DeepCopy()
	setControlPlaneStatus(&cp, clusterName, desiredReplicas, machines, bootstrapState, caRotationState, upgradeState)
	if err := r.Status().Patch(ctx, &cp, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}

	requeueAfter := bootstrapState.requeueAfter
	if caRotationState.requeueAfter > 0 && (requeueAfter == 0 || caRotationState.requeueAfter < requeueAfter) {
		requeueAfter = caRotationState.requeueAfter
	}
	if upgradeState.requeueAfter > 0 && (requeueAfter == 0 || upgradeState.requeueAfter < requeueAfter) {
		requeueAfter = upgradeState.requeueAfter
	}
	if scaleDownPending && requeueAfter == 0 {
		requeueAfter = controlPlaneScaleDownRequeue
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *TartControlPlaneReconciler) reconcilePaused(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane) (ctrl.Result, error) {
	original := cp.DeepCopy()
	setPausedCondition(&cp.Status.Conditions, true, cp.Generation)
	if err := r.Status().Patch(ctx, cp, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TartControlPlaneReconciler) reconcileDeleting(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane) (ctrl.Result, error) {
	original := cp.DeepCopy()
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneDeletingCondition, metav1.ConditionTrue, "Deleting", "The control plane is being deleted; no new Machine or Host allocation is started.", cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneAvailableCondition, metav1.ConditionFalse, "Deleting", "The control plane is being deleted.", cp.Generation)
	setPausedCondition(&cp.Status.Conditions, false, cp.Generation)
	cp.Status.ObservedGeneration = cp.Generation
	if err := r.Status().Patch(ctx, cp, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TartControlPlaneReconciler) reportFailure(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, report error) (ctrl.Result, error) {
	failure := &controlPlaneFailure{reason: "ReconcileFailed", message: "The control plane cannot proceed until its dependencies are available."}
	var typedFailure *controlPlaneFailure
	if failureValue, ok := errors.AsType[*controlPlaneFailure](report); ok {
		typedFailure = failureValue
		failure = typedFailure
	}
	original := cp.DeepCopy()
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneAvailableCondition, metav1.ConditionFalse, failure.reason, failure.message, cp.Generation)
	setPausedCondition(&cp.Status.Conditions, false, cp.Generation)
	cp.Status.ObservedGeneration = cp.Generation
	if err := r.Status().Patch(ctx, cp, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func controlPlaneClusterName(cp *controlplanev1alpha1.TartControlPlane) (string, error) {
	if name := cp.Labels[clusterv1.ClusterNameLabel]; name != "" {
		return name, nil
	}
	for _, owner := range cp.OwnerReferences {
		if owner.APIVersion == clusterv1.GroupVersion.String() && owner.Kind == "Cluster" && owner.Name != "" {
			return owner.Name, nil
		}
	}
	return "", &controlPlaneFailure{
		reason:  reasonClusterUnavailable,
		message: "The TartControlPlane has no reference to a CAPI Cluster.",
	}
}

func desiredControlPlaneReplicas(cp *controlplanev1alpha1.TartControlPlane) (int32, error) {
	if cp.Spec.Replicas == nil {
		return 1, nil
	}
	if *cp.Spec.Replicas < 1 {
		return 0, &controlPlaneFailure{
			reason:  "InvalidSpec",
			message: "The control-plane replica count must be at least one to preserve an etcd member.",
		}
	}
	return *cp.Spec.Replicas, nil
}

func (r *TartControlPlaneReconciler) getTartCluster(ctx context.Context, cluster *clusterv1.Cluster) (*infrav1alpha1.TartCluster, error) {
	ref := cluster.Spec.InfrastructureRef
	if ref.APIGroup != infrav1alpha1.GroupVersion.Group || ref.Kind != tartClusterKind || ref.Name == "" {
		return nil, &controlPlaneFailure{
			reason:  reasonClusterUnavailable,
			message: "The CAPI Cluster does not reference a TartCluster infrastructure resource.",
		}
	}
	var tartCluster infrav1alpha1.TartCluster
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: ref.Name}, &tartCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, &controlPlaneFailure{
				reason:  reasonClusterUnavailable,
				message: "The referenced TartCluster is not available yet.",
			}
		}
		return nil, err
	}
	if tartCluster.Spec.ClusterID == "" || tartCluster.Status.ActiveSecretGeneration < 1 {
		return nil, &controlPlaneFailure{
			reason:  reasonSecretBundleUnavailable,
			message: "The TartCluster identity and active secret bundle are not ready yet.",
		}
	}
	if _, err := parseClusterID(tartCluster.Spec.ClusterID); err != nil {
		return nil, &controlPlaneFailure{
			reason:  reasonSecretBundleUnavailable,
			message: "The TartCluster identity is invalid.",
		}
	}
	return &tartCluster, nil
}

func (r *TartControlPlaneReconciler) validateActiveBundle(ctx context.Context, cluster *infrav1alpha1.TartCluster) error {
	generation := cluster.Status.ActiveSecretGeneration
	clusterID, err := parseClusterID(cluster.Spec.ClusterID)
	if err != nil {
		return &controlPlaneFailure{
			reason:  reasonSecretBundleUnavailable,
			message: "The TartCluster identity is invalid.",
		}
	}
	name, err := controlplane.BundleName(cluster.Name, clusterID, generation)
	if err != nil {
		return &controlPlaneFailure{
			reason:  reasonSecretBundleUnavailable,
			message: "The active cluster secret bundle identity is invalid.",
		}
	}
	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return &controlPlaneFailure{
				reason:  reasonSecretBundleUnavailable,
				message: "The active cluster secret bundle is not available yet.",
			}
		}
		return err
	}
	if err := controlplane.ValidateBundleSecretContract(&secret, cluster.Namespace, cluster.Name, clusterID, generation, controlplane.BundleStateActive, cluster.UID); err != nil {
		return &controlPlaneFailure{
			reason:  reasonSecretBundleUnavailable,
			message: "The active cluster secret bundle does not satisfy its identity contract.",
		}
	}
	if err := controlplane.ValidateBundleData(secret.Data, clusterID); err != nil {
		return &controlPlaneFailure{
			reason:  reasonSecretBundleUnavailable,
			message: "The active cluster secret bundle data is invalid.",
		}
	}
	return nil
}

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
			reason:  reasonBootstrapTemplateInvalid,
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
			return nil, &controlPlaneFailure{reason: reasonMachineNameInvalid, message: "A deterministic control-plane Machine name is invalid."}
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
			return nil, &controlPlaneFailure{reason: reasonMachineNameInvalid, message: bootstrapConfigNameInvalidMessage}
		}
		if err := validateMachineReference(machine, cp, clusterName, machineName, bootstrapName, cp.Spec.Version); err != nil {
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
		if !hasControllerOwner(&machines[index], cp, controlplanev1alpha1.GroupVersion.String(), tartControlPlaneKind) {
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
	if reflect.DeepEqual(machine.Spec.ReadinessGates, expectedReadinessGates) && reflect.DeepEqual(machine.Spec.Taints, expectedTaints) && reflect.DeepEqual(machine.Spec.Deletion, expectedDeletion) {
		return nil
	}
	original := machine.DeepCopy()
	machine.Spec.ReadinessGates = expectedReadinessGates
	machine.Spec.Taints = expectedTaints
	machine.Spec.Deletion = expectedDeletion
	return r.Patch(ctx, machine, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{}))
}

func machineDeletionSpec(spec controlplanev1alpha1.TartControlPlaneMachineTemplateDeletionSpec) clusterv1.MachineDeletionSpec {
	return clusterv1.MachineDeletionSpec{
		NodeDrainTimeoutSeconds:        cloneInt32(spec.NodeDrainTimeoutSeconds),
		NodeVolumeDetachTimeoutSeconds: cloneInt32(spec.NodeVolumeDetachTimeoutSeconds),
		NodeDeletionTimeoutSeconds:     cloneInt32(spec.NodeDeletionTimeoutSeconds),
	}
}

func cloneInt32(value *int32) *int32 {
	if value == nil {
		return nil
	}
	return new(*value)
}

// reconcileScaleDownは一度に一台だけを選び、etcd member removalが観測できるまでCAPI Machineのpre-terminate hookを保持する。
func (r *TartControlPlaneReconciler) reconcileScaleDown(ctx context.Context, machines []clusterv1.Machine, desired int32) (bool, error) {
	if int32(len(machines)) <= desired {
		return false, nil
	}
	candidates := make([]clusterv1.Machine, 0, len(machines))
	deleting := make([]clusterv1.Machine, 0, len(machines))
	for index := range machines {
		if machines[index].DeletionTimestamp.IsZero() {
			candidates = append(candidates, machines[index])
		} else {
			deleting = append(deleting, machines[index])
		}
	}
	if len(deleting) > 0 {
		slices.SortFunc(deleting, compareControlPlaneMachineOrder)
		target := &deleting[0]
		if target.Annotations[controlPlaneEtcdDeleteHook] != controlPlaneEtcdDeleteHookValue {
			if err := r.ensureEtcdDeleteHook(ctx, target); err != nil {
				return false, err
			}
			return true, nil
		}
		return r.reconcileEtcdMemberRemoval(ctx, target, machines)
	}
	if len(candidates) == 0 {
		return false, nil
	}
	slices.SortFunc(candidates, compareControlPlaneMachineOrder)
	target := &candidates[0]
	if err := r.ensureEtcdDeleteHook(ctx, target); err != nil {
		return false, err
	}
	if err := r.Delete(ctx, target); err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}
	return true, nil
}

func compareControlPlaneMachineOrder(left, right clusterv1.Machine) int {
	leftOrdinal, leftErr := strconv.ParseInt(left.Labels[controlPlaneOrdinalLabel], 10, 32)
	rightOrdinal, rightErr := strconv.ParseInt(right.Labels[controlPlaneOrdinalLabel], 10, 32)
	if leftErr == nil && rightErr == nil && leftOrdinal != rightOrdinal {
		return int(rightOrdinal - leftOrdinal)
	}
	return strings.Compare(right.Name, left.Name)
}

func (r *TartControlPlaneReconciler) ensureEtcdDeleteHook(ctx context.Context, machine *clusterv1.Machine) error {
	if machine.Annotations[controlPlaneEtcdDeleteHook] == controlPlaneEtcdDeleteHookValue {
		return nil
	}
	original := machine.DeepCopy()
	machine.Annotations = maps.Clone(machine.Annotations)
	if machine.Annotations == nil {
		machine.Annotations = make(map[string]string)
	}
	machine.Annotations[controlPlaneEtcdDeleteHook] = controlPlaneEtcdDeleteHookValue
	return r.Patch(ctx, machine, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{}))
}

type etcdMachineObservation struct {
	machine *clusterv1.Machine
	host    *infrav1alpha1.TartHost
	config  []byte
	status  talos.EtcdStatus
}

func (r *TartControlPlaneReconciler) reconcileEtcdMemberRemoval(ctx context.Context, target *clusterv1.Machine, machines []clusterv1.Machine) (bool, error) {
	if !machineWaitingForPreTerminateHook(target) {
		// CAPI Machine controllerがdrainとvolume detachを終えてpre-terminate hook待ちに
		// 遷移するまでは、etcd member removalを開始しない。
		return true, nil
	}
	annotatedID, annotated, handled, err := r.reconcileAnnotatedEtcdMember(ctx, target, machines)
	if handled || err != nil {
		return handled, err
	}

	observations, observationsReady := r.observeEtcdMachineObservations(ctx, machines)
	if !observationsReady {
		return true, nil
	}
	targetObservation, healthyMembers, observationsUnique := summarizeEtcdObservations(observations, target.UID)
	if !observationsUnique || targetObservation == nil {
		return true, nil
	}
	memberID := targetObservation.status.MemberID
	if annotated {
		parsed, err := strconv.ParseUint(annotatedID, 10, 64)
		if err != nil || parsed != memberID {
			return false, &controlPlaneFailure{reason: "EtcdMemberIdentityChanged", message: "The deleting control-plane Machine etcd member identity no longer matches Talos observation."}
		}
	} else if err := r.annotateEtcdMemberID(ctx, target, memberID); err != nil {
		return true, err
	}

	survivor := firstEtcdObservation(observations, target.UID)
	if survivor == nil {
		return true, nil
	}
	members, err := r.observeEtcdMembersFromConfiguration(ctx, survivor.host, survivor.config)
	if err != nil {
		return true, nil //nolint:nilerr // removal must wait until a survivor exposes stable membership.
	}
	if !canRemoveObservedEtcdMember(members, observations, memberID, healthyMembers, targetObservation) {
		return true, nil
	}
	return r.removeObservedEtcdMember(ctx, survivor, memberID)
}

func (r *TartControlPlaneReconciler) observeEtcdMachineObservations(ctx context.Context, machines []clusterv1.Machine) ([]etcdMachineObservation, bool) {
	observations := make([]etcdMachineObservation, 0, len(machines))
	for index := range machines {
		machine := &machines[index]
		hostObject, configuration, err := r.observeMachineIdentity(ctx, machine)
		if err != nil {
			return nil, false
		}
		status, err := r.observeEtcdStatus(ctx, hostObject, configuration)
		if err != nil || status.MemberID == 0 {
			return nil, false
		}
		observations = append(observations, etcdMachineObservation{machine: machine, host: hostObject, config: configuration, status: status})
	}
	return observations, true
}

func summarizeEtcdObservations(observations []etcdMachineObservation, targetUID types.UID) (*etcdMachineObservation, int, bool) {
	var targetObservation *etcdMachineObservation
	memberIDs := make(map[uint64]struct{}, len(observations))
	healthyMembers := 0
	for index := range observations {
		observation := &observations[index]
		if _, exists := memberIDs[observation.status.MemberID]; exists {
			return nil, 0, false
		}
		memberIDs[observation.status.MemberID] = struct{}{}
		if etcdStatusHealthy(observation.status) {
			healthyMembers++
		}
		if observation.machine.UID == targetUID {
			targetObservation = observation
		}
	}
	return targetObservation, healthyMembers, true
}

func canRemoveObservedEtcdMember(members []talos.EtcdMember, observations []etcdMachineObservation, memberID uint64, healthyMembers int, targetObservation *etcdMachineObservation) bool {
	if len(members) != len(observations) || !containsEtcdMember(members, memberID) || !allEtcdMembersAreVoting(members) {
		return false
	}
	for _, observation := range observations {
		if !containsEtcdMember(members, observation.status.MemberID) {
			return false
		}
	}
	return controlplane.CanRemoveMember(controlplane.RemovalObservation{
		MemberCount:          len(members),
		HealthyMemberCount:   healthyMembers,
		TargetHealthy:        etcdStatusHealthy(targetObservation.status),
		TargetHealthObserved: true,
	})
}

func (r *TartControlPlaneReconciler) removeObservedEtcdMember(ctx context.Context, survivor *etcdMachineObservation, memberID uint64) (bool, error) {
	removalClient, err := r.dialAuthenticated(ctx, survivor.host, survivor.config)
	if err != nil {
		return true, nil //nolint:nilerr // a survivor connection failure must retain the deletion hook.
	}
	removeErr := removalClient.RemoveEtcdMember(ctx, memberID)
	if closeErr := removalClient.Close(); closeErr != nil && removeErr == nil {
		return true, closeErr
	}
	if removeErr != nil {
		return true, nil //nolint:nilerr // Talos removal is retried from observed membership on the next reconcile.
	}
	return true, nil
}

func (r *TartControlPlaneReconciler) reconcileAnnotatedEtcdMember(ctx context.Context, target *clusterv1.Machine, machines []clusterv1.Machine) (string, bool, bool, error) {
	annotatedID, annotated := target.Annotations[controlPlaneEtcdMemberID]
	if !annotated {
		return "", false, false, nil
	}
	memberID, err := strconv.ParseUint(annotatedID, 10, 64)
	if err != nil || memberID == 0 {
		return annotatedID, true, false, &controlPlaneFailure{reason: "EtcdMemberIdentityInvalid", message: "The deleting control-plane Machine has an invalid etcd member identity annotation."}
	}
	survivor := firstControlPlaneSurvivor(machines, target.UID)
	if survivor == nil {
		return annotatedID, true, true, nil
	}
	members, err := r.observeEtcdMembers(ctx, survivor)
	if err != nil {
		return annotatedID, true, true, nil //nolint:nilerr // etcd observation is transient while the deleting Machine is waiting.
	}
	if !containsEtcdMember(members, memberID) {
		result, removeErr := r.removeEtcdDeleteHook(ctx, target)
		return annotatedID, true, result, removeErr
	}
	return annotatedID, true, false, nil
}

func etcdStatusHealthy(status talos.EtcdStatus) bool {
	return status.MemberID != 0 && status.Leader != 0 && len(status.Errors) == 0
}

func machineWaitingForPreTerminateHook(machine *clusterv1.Machine) bool {
	if machine == nil || machine.DeletionTimestamp.IsZero() {
		return false
	}
	condition := meta.FindStatusCondition(machine.Status.Conditions, clusterv1.MachineDeletingCondition)
	return condition != nil && condition.Status == metav1.ConditionTrue && condition.Reason == clusterv1.MachineDeletingWaitingForPreTerminateHookReason
}

func (r *TartControlPlaneReconciler) observeMachineIdentity(ctx context.Context, machine *clusterv1.Machine) (*infrav1alpha1.TartHost, []byte, error) {
	if machine == nil {
		return nil, nil, errors.New("control-plane Machine is nil")
	}
	ref := machine.Spec.InfrastructureRef
	if ref.APIGroup != infrav1alpha1.GroupVersion.Group || ref.Kind != tartMachineKind || ref.Name == "" {
		return nil, nil, errors.New("control-plane Machine infrastructure reference is invalid")
	}
	provider := &infrav1alpha1.TartMachine{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: ref.Name}, provider); err != nil {
		return nil, nil, err
	}
	if err := validateProviderOwner(provider, machine, clusterv1.GroupVersion.String(), tartMachineKind); err != nil {
		return nil, nil, err
	}
	if provider.Status.HostRef == nil || provider.Status.HostRef.Name == "" {
		return nil, nil, errors.New("control-plane Machine Host reference is unavailable")
	}
	hostObject := &infrav1alpha1.TartHost{}
	if err := r.Get(ctx, client.ObjectKey{Name: provider.Status.HostRef.Name}, hostObject); err != nil {
		return nil, nil, err
	}
	if hostObject.Spec.ConsumerRef == nil || hostObject.Spec.ConsumerRef.UID != provider.UID {
		return nil, nil, errors.New("control-plane Host binding does not match provider Machine")
	}
	endpoint := hostTalosEndpoint(hostObject)
	if endpoint == "" {
		return nil, nil, errors.New("control-plane Host Talos endpoint is unavailable")
	}
	configuration, err := (&TartMachineReconciler{Client: r.Client}).bootstrapConfiguration(ctx, provider)
	if err != nil {
		return nil, nil, err
	}
	return hostObject, configuration, nil
}

func (r *TartControlPlaneReconciler) observeEtcdStatus(ctx context.Context, hostObject *infrav1alpha1.TartHost, configuration []byte) (talos.EtcdStatus, error) {
	client, err := r.dialAuthenticated(ctx, hostObject, configuration)
	if err != nil {
		return talos.EtcdStatus{}, err
	}
	status, statusErr := client.EtcdStatus(ctx)
	if closeErr := client.Close(); closeErr != nil && statusErr == nil {
		return talos.EtcdStatus{}, closeErr
	}
	return status, statusErr
}

func (r *TartControlPlaneReconciler) observeEtcdMembers(ctx context.Context, machine *clusterv1.Machine) ([]talos.EtcdMember, error) {
	hostObject, configuration, err := r.observeMachineIdentity(ctx, machine)
	if err != nil {
		return nil, err
	}
	return r.observeEtcdMembersFromConfiguration(ctx, hostObject, configuration)
}

func (r *TartControlPlaneReconciler) observeEtcdMembersFromConfiguration(ctx context.Context, hostObject *infrav1alpha1.TartHost, configuration []byte) ([]talos.EtcdMember, error) {
	client, err := r.dialAuthenticated(ctx, hostObject, configuration)
	if err != nil {
		return nil, err
	}
	members, membersErr := client.EtcdMembers(ctx)
	if closeErr := client.Close(); closeErr != nil && membersErr == nil {
		return nil, closeErr
	}
	return members, membersErr
}

func (r *TartControlPlaneReconciler) dialAuthenticated(ctx context.Context, hostObject *infrav1alpha1.TartHost, configuration []byte) (*talos.Client, error) {
	connectionContext, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return talos.DialAuthenticatedFromConfiguration(connectionContext, hostTalosEndpoint(hostObject), configuration)
}

func (r *TartControlPlaneReconciler) annotateEtcdMemberID(ctx context.Context, machine *clusterv1.Machine, memberID uint64) error {
	original := machine.DeepCopy()
	machine.Annotations = maps.Clone(machine.Annotations)
	if machine.Annotations == nil {
		machine.Annotations = make(map[string]string)
	}
	machine.Annotations[controlPlaneEtcdMemberID] = strconv.FormatUint(memberID, 10)
	return r.Patch(ctx, machine, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{}))
}

func firstControlPlaneSurvivor(machines []clusterv1.Machine, targetUID types.UID) *clusterv1.Machine {
	for index := range machines {
		if machines[index].UID != targetUID && machines[index].DeletionTimestamp.IsZero() {
			return &machines[index]
		}
	}
	return nil
}

func firstEtcdObservation(observations []etcdMachineObservation, targetUID types.UID) *etcdMachineObservation {
	for index := range observations {
		if observations[index].machine.UID != targetUID {
			return &observations[index]
		}
	}
	return nil
}

func containsEtcdMember(members []talos.EtcdMember, memberID uint64) bool {
	for _, member := range members {
		if member.ID == memberID {
			return true
		}
	}
	return false
}

func allEtcdMembersAreVoting(members []talos.EtcdMember) bool {
	for _, member := range members {
		if member.Learner {
			return false
		}
	}
	return true
}

func (r *TartControlPlaneReconciler) removeEtcdDeleteHook(ctx context.Context, machine *clusterv1.Machine) (bool, error) {
	original := machine.DeepCopy()
	machine.Annotations = maps.Clone(machine.Annotations)
	delete(machine.Annotations, controlPlaneEtcdDeleteHook)
	delete(machine.Annotations, controlPlaneEtcdMemberID)
	if err := r.Patch(ctx, machine, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return true, err
	}
	return false, nil
}

func (r *TartControlPlaneReconciler) reconcileControlPlaneBootstrap(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, cluster *clusterv1.Cluster, machines []clusterv1.Machine) (controlPlaneBootstrapState, error) {
	state := controlPlaneBootstrapState{
		reason:       "MachinesUnavailable",
		message:      "The first control-plane Machine is not running the desired Talos version yet.",
		requeueAfter: 30 * time.Second,
	}
	firstName, err := controlPlaneChildName(cp.Name, 0, "")
	if err != nil {
		return controlPlaneBootstrapState{}, &controlPlaneFailure{reason: reasonMachineNameInvalid, message: "A deterministic control-plane Machine name is invalid."}
	}
	var firstMachine *clusterv1.Machine
	for index := range machines {
		if machines[index].Name == firstName {
			firstMachine = &machines[index]
			break
		}
	}
	if firstMachine == nil {
		return state, nil
	}
	return r.reconcileFirstControlPlane(ctx, cp, cluster, firstMachine, state)
}

func (r *TartControlPlaneReconciler) reconcileFirstControlPlane(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, cluster *clusterv1.Cluster, machine *clusterv1.Machine, state controlPlaneBootstrapState) (controlPlaneBootstrapState, error) {
	observation, state, ready, err := r.observeFirstControlPlane(ctx, machine, state)
	if err != nil {
		return controlPlaneBootstrapState{}, err
	}
	if !ready {
		return state, nil
	}
	return r.reconcileFirstControlPlaneTalos(ctx, cp, cluster, observation, state)
}

type firstControlPlaneObservation struct {
	endpoint      string
	configuration []byte
}

func (r *TartControlPlaneReconciler) observeFirstControlPlane(ctx context.Context, machine *clusterv1.Machine, state controlPlaneBootstrapState) (firstControlPlaneObservation, controlPlaneBootstrapState, bool, error) {
	if machine == nil || machine.Spec.InfrastructureRef.APIGroup != infrav1alpha1.GroupVersion.Group || machine.Spec.InfrastructureRef.Kind != tartMachineKind || machine.Spec.InfrastructureRef.Name == "" {
		return firstControlPlaneObservation{}, controlPlaneBootstrapState{}, false, &controlPlaneFailure{reason: reasonMachineSpecMismatch, message: "The first control-plane Machine has an invalid infrastructure reference."}
	}
	var providerMachine infrav1alpha1.TartMachine
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: machine.Spec.InfrastructureRef.Name}, &providerMachine); err != nil {
		if apierrors.IsNotFound(err) {
			return firstControlPlaneObservation{}, state, false, nil
		}
		return firstControlPlaneObservation{}, controlPlaneBootstrapState{}, false, err
	}
	if err := validateProviderOwner(&providerMachine, machine, clusterv1.GroupVersion.String(), tartMachineKind); err != nil {
		return firstControlPlaneObservation{}, controlPlaneBootstrapState{}, false, err
	}
	ready := meta.FindStatusCondition(providerMachine.Status.Conditions, infrav1alpha1.TartMachineReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		return firstControlPlaneObservation{}, state, false, nil
	}
	if providerMachine.Status.HostRef == nil {
		state.reason = "HostUnavailable"
		state.message = "The first control-plane Machine has no observed TartHost binding yet."
		return firstControlPlaneObservation{}, state, false, nil
	}

	var providerHost infrav1alpha1.TartHost
	if err := r.Get(ctx, client.ObjectKey{Name: providerMachine.Status.HostRef.Name}, &providerHost); err != nil {
		if apierrors.IsNotFound(err) {
			state.reason = "HostUnavailable"
			state.message = "The first control-plane Machine Host is not available yet."
			return firstControlPlaneObservation{}, state, false, nil
		}
		return firstControlPlaneObservation{}, controlPlaneBootstrapState{}, false, err
	}
	if providerHost.Spec.ConsumerRef == nil || providerHost.Spec.ConsumerRef.UID != providerMachine.UID {
		return firstControlPlaneObservation{}, controlPlaneBootstrapState{}, false, &controlPlaneFailure{reason: "HostBindingMismatch", message: "The first control-plane Machine Host binding does not match the provider Machine."}
	}
	endpoint := hostTalosEndpoint(&providerHost)
	if endpoint == "" {
		state.reason = "EndpointUnavailable"
		state.message = "The first control-plane Machine has no reachable Talos endpoint yet."
		return firstControlPlaneObservation{}, state, false, nil
	}
	configuration, err := (&TartMachineReconciler{Client: r.Client}).bootstrapConfiguration(ctx, &providerMachine)
	if err != nil {
		if errors.Is(err, errBootstrapDataUnavailable) {
			state.reason = "BootstrapDataUnavailable"
			state.message = "The immutable Bootstrap Secret is not available for the first control-plane Machine yet."
			return firstControlPlaneObservation{}, state, false, nil
		}
		return firstControlPlaneObservation{}, controlPlaneBootstrapState{}, false, err
	}
	return firstControlPlaneObservation{endpoint: endpoint, configuration: configuration}, state, true, nil
}

func (r *TartControlPlaneReconciler) reconcileFirstControlPlaneTalos(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, cluster *clusterv1.Cluster, observation firstControlPlaneObservation, state controlPlaneBootstrapState) (controlPlaneBootstrapState, error) {
	authenticated, err := talos.DialAuthenticatedFromConfiguration(ctx, observation.endpoint, observation.configuration)
	if err != nil {
		state.reason = "TalosUnavailable"
		state.message = "The authenticated Talos API is not reachable on the first control-plane Machine."
		return state, nil //nolint:nilerr // an unavailable node is a normal reconcile observation.
	}
	etcdStatus, etcdErr := authenticated.EtcdStatus(ctx)
	if etcdErr == nil && etcdStatusHealthy(etcdStatus) {
		kubeconfig, kubeconfigErr := authenticated.Kubeconfig(ctx)
		if kubeconfigErr != nil {
			if closeErr := authenticated.Close(); closeErr != nil {
				ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
			}
			return controlPlaneBootstrapState{
				etcdReady:    true,
				reason:       reasonWorkloadAPIUnavailable,
				message:      "Talos etcd is healthy, but the workload kubeconfig is not available yet.",
				requeueAfter: 30 * time.Second,
			}, nil
		}
		if apiErr := workloadAPIReady(ctx, kubeconfig, cluster.Spec.ControlPlaneEndpoint); apiErr != nil {
			if closeErr := authenticated.Close(); closeErr != nil {
				ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
			}
			return controlPlaneBootstrapState{
				etcdReady:    true,
				reason:       reasonWorkloadAPIUnavailable,
				message:      "Talos etcd is healthy, but the workload Kubernetes API is not ready yet.",
				requeueAfter: 30 * time.Second,
			}, nil
		}
		if kubeconfigErr := r.ensureKubeconfigSecret(ctx, cluster, kubeconfig); kubeconfigErr != nil {
			if closeErr := authenticated.Close(); closeErr != nil {
				ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
			}
			return controlPlaneBootstrapState{
				etcdReady:     true,
				workloadReady: false,
				reason:        "KubeconfigUnavailable",
				message:       "The workload Kubernetes API is ready, but the workload kubeconfig Secret could not be persisted.",
				requeueAfter:  15 * time.Second,
			}, kubeconfigErr
		}
		if closeErr := authenticated.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		return controlPlaneBootstrapState{
			initialized:   true,
			etcdReady:     true,
			workloadReady: true,
			reason:        "EtcdClusterAvailable",
			message:       "The workload Kubernetes API is ready and the first control-plane Machine reports a healthy etcd member and leader.",
		}, nil
	}
	if cp.Status.Initialization.ControlPlaneInitialized != nil && *cp.Status.Initialization.ControlPlaneInitialized {
		// 初回bootstrap後は、etcd観測が失敗してもBootstrap RPCを再実行しない。
		// 再bootstrapは既存clusterのmembershipやdataを壊す可能性があるため、
		// Talosとworkload APIの復旧を観測しながら安全停止する。
		if closeErr := authenticated.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		return controlPlaneBootstrapState{
			reason:       "EtcdClusterUnavailable",
			message:      "The control plane was initialized previously, but a healthy etcd member and leader are not currently observed.",
			requeueAfter: 30 * time.Second,
		}, nil
	}

	bootstrapErr := authenticated.Bootstrap(ctx)
	if closeErr := authenticated.Close(); closeErr != nil {
		ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
	}
	if bootstrapErr != nil && status.Code(bootstrapErr) != codes.AlreadyExists {
		return controlPlaneBootstrapState{
			reason:       "EtcdBootstrapFailed",
			message:      "The first control-plane Machine could not start the Talos etcd bootstrap.",
			requeueAfter: 30 * time.Second,
		}, nil
	}
	return controlPlaneBootstrapState{
		reason:       "Bootstrapping",
		message:      "Talos etcd bootstrap was requested; waiting for a healthy member and leader.",
		requeueAfter: 15 * time.Second,
	}, nil
}

func (r *TartControlPlaneReconciler) ensureKubeconfigSecret(ctx context.Context, cluster *clusterv1.Cluster, kubeconfig []byte) error {
	if cluster == nil || cluster.Namespace == "" || cluster.Name == "" || cluster.UID == "" || len(kubeconfig) == 0 {
		return errors.New("workload kubeconfig identity or data is incomplete")
	}
	expected := &corev1.Secret{
		Namespace:       cluster.Namespace,
		Name:            cluster.Name + "-kubeconfig",
		Labels:          map[string]string{clusterv1.ClusterNameLabel: cluster.Name},
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(cluster, clusterv1.GroupVersion.String(), "Cluster")},
		Type:            clusterv1.ClusterSecretType,
		Data:            map[string][]byte{"value": bytes.Clone(kubeconfig)},
	}

	actual := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: expected.Namespace, Name: expected.Name}, actual)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, expected)
	}
	if err != nil {
		return err
	}
	if actual.Type != clusterv1.ClusterSecretType || actual.Labels[clusterv1.ClusterNameLabel] != cluster.Name || !hasControllerOwner(actual, cluster, clusterv1.GroupVersion.String(), "Cluster") {
		return errors.New("existing workload kubeconfig Secret does not satisfy the CAPI contract")
	}
	if bytes.Equal(actual.Data["value"], kubeconfig) && len(actual.Data) == 1 {
		return nil
	}
	original := actual.DeepCopy()
	actual.Data = map[string][]byte{"value": bytes.Clone(kubeconfig)}
	actual.Type = clusterv1.ClusterSecretType
	return r.Patch(ctx, actual, client.MergeFrom(original))
}

func workloadAPIReady(ctx context.Context, kubeconfig []byte, endpoint clusterv1.APIEndpoint) error {
	if len(kubeconfig) == 0 {
		return errors.New("workload kubeconfig is empty")
	}
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return fmt.Errorf("parse workload kubeconfig: %w", err)
	}
	if endpoint.IsValid() {
		config.Host = "https://" + endpoint.String()
	}
	config.Timeout = 10 * time.Second
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create workload Kubernetes client: %w", err)
	}
	if err := clientset.Discovery().RESTClient().Get().AbsPath("/readyz").Do(ctx).Error(); err != nil {
		return fmt.Errorf("check workload Kubernetes API readiness: %w", err)
	}
	return nil
}

func (r *TartControlPlaneReconciler) createMachine(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, clusterName string, ordinal int32, failureDomain, name string) (*clusterv1.Machine, error) {
	bootstrapName, err := bootstrapConfigName(cp.Name, ordinal)
	if err != nil {
		return nil, &controlPlaneFailure{reason: reasonMachineNameInvalid, message: bootstrapConfigNameInvalidMessage}
	}
	objectLabels, annotations := controlPlaneMetadata(cp.Spec.MachineTemplate.ObjectMeta, clusterName, cp.Name, ordinal)
	machine := &clusterv1.Machine{
		APIVersion:      clusterv1.GroupVersion.String(),
		Kind:            capiMachineKind,
		Name:            name,
		Namespace:       cp.Namespace,
		Labels:          objectLabels,
		Annotations:     annotations,
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(cp, controlplanev1alpha1.GroupVersion.String(), tartControlPlaneKind)},
		Spec: clusterv1.MachineSpec{
			ClusterName:    clusterName,
			Version:        cp.Spec.Version,
			FailureDomain:  failureDomain,
			ReadinessGates: slices.Clone(cp.Spec.MachineTemplate.Spec.ReadinessGates),
			Taints:         slices.Clone(cp.Spec.MachineTemplate.Spec.Taints),
			Deletion:       machineDeletionSpec(cp.Spec.MachineTemplate.Spec.Deletion),
			Bootstrap: clusterv1.Bootstrap{ConfigRef: clusterv1.ContractVersionedObjectReference{
				APIGroup: bootstrapv1alpha1.GroupVersion.Group,
				Kind:     tartBootstrapConfigKind,
				Name:     bootstrapName,
			}},
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				APIGroup: infrav1alpha1.GroupVersion.Group,
				Kind:     tartMachineKind,
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
		Kind:            tartMachineKind,
		Name:            machineName,
		Namespace:       cp.Namespace,
		Labels:          providerLabels,
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(machine, clusterv1.GroupVersion.String(), capiMachineKind)},
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
	if err := validateProviderOwner(&tartMachine, machine, infrav1alpha1.GroupVersion.String(), tartMachineKind); err != nil {
		return err
	}
	if !reflect.DeepEqual(tartMachine.Spec.HostSelector, expectedMachine.Spec.HostSelector) || tartMachine.Spec.Image != expectedMachine.Spec.Image {
		return &controlPlaneFailure{reason: reasonMachineSpecMismatch, message: "The existing TartMachine does not match its immutable control-plane template."}
	}

	bootstrapName, err := bootstrapConfigName(cp.Name, ordinal)
	if err != nil {
		return &controlPlaneFailure{reason: reasonMachineNameInvalid, message: bootstrapConfigNameInvalidMessage}
	}
	bootstrapLabels, _ := controlPlaneMetadata(bootstrapTemplate.Spec.Template.ObjectMeta, clusterName, cp.Name, ordinal)
	expectedBootstrap := &bootstrapv1alpha1.TartBootstrapConfig{
		APIVersion:      bootstrapv1alpha1.GroupVersion.String(),
		Kind:            tartBootstrapConfigKind,
		Name:            bootstrapName,
		Namespace:       cp.Namespace,
		Labels:          bootstrapLabels,
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(machine, clusterv1.GroupVersion.String(), capiMachineKind)},
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
	if err := validateProviderOwner(&bootstrapConfig, machine, clusterv1.GroupVersion.String(), tartBootstrapConfigKind); err != nil {
		return err
	}
	actualRef := bootstrapConfig.Spec.ConfigPatchesSecretRef
	expectedRef := expectedBootstrap.Spec.ConfigPatchesSecretRef
	if (actualRef == nil) != (expectedRef == nil) || (actualRef != nil && actualRef.Name != expectedRef.Name) {
		return &controlPlaneFailure{reason: "BootstrapConfigMismatch", message: "The existing TartBootstrapConfig does not match its immutable template."}
	}
	return nil
}

func validateMachineReference(machine *clusterv1.Machine, cp *controlplanev1alpha1.TartControlPlane, clusterName, machineName, bootstrapName, version string) error {
	if !hasControllerOwner(machine, cp, controlplanev1alpha1.GroupVersion.String(), tartControlPlaneKind) || machine.Spec.ClusterName != clusterName || machine.Spec.Version != version || machine.Spec.InfrastructureRef.APIGroup != infrav1alpha1.GroupVersion.Group || machine.Spec.InfrastructureRef.Kind != tartMachineKind || machine.Spec.InfrastructureRef.Name != machineName || machine.Spec.Bootstrap.ConfigRef.APIGroup != bootstrapv1alpha1.GroupVersion.Group || machine.Spec.Bootstrap.ConfigRef.Kind != tartBootstrapConfigKind || machine.Spec.Bootstrap.ConfigRef.Name != bootstrapName {
		return &controlPlaneFailure{reason: reasonMachineSpecMismatch, message: "The existing control-plane Machine does not match the TartControlPlane references."}
	}
	return nil
}

func validateProviderOwner(object metav1.Object, machine *clusterv1.Machine, apiVersion, kind string) error {
	if len(object.GetOwnerReferences()) != 1 || !hasControllerOwner(object, machine, apiVersion, kind) {
		return &controlPlaneFailure{reason: "MachineOwnershipMismatch", message: "A provider resource is not owned by its corresponding CAPI Machine."}
	}
	return nil
}

func hasControllerOwner(object metav1.Object, owner metav1.Object, apiVersion, kind string) bool {
	for _, reference := range object.GetOwnerReferences() {
		if reference.APIVersion == apiVersion && reference.Kind == kind && reference.Name == owner.GetName() && reference.UID == owner.GetUID() && reference.Controller != nil && *reference.Controller {
			return true
		}
	}
	return false
}

func controllerOwnerReference(owner metav1.Object, apiVersion, kind string) metav1.OwnerReference {
	controller := true
	blockOwnerDeletion := true
	return metav1.OwnerReference{
		APIVersion:         apiVersion,
		Kind:               kind,
		Name:               owner.GetName(),
		UID:                owner.GetUID(),
		Controller:         &controller,
		BlockOwnerDeletion: &blockOwnerDeletion,
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
		availableReason = reasonWorkloadAPIUnavailable
		availableMessage = "The workload Kubernetes API is not available yet."
	} else if controlPlaneReady {
		availableReason = "Available"
		availableMessage = "All desired control-plane Machines and the workload Kubernetes API are available."
	}
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneAvailableCondition, conditionStatus(controlPlaneReady), availableReason, availableMessage, cp.Generation)
	// Kubernetes version upgradeが未収束の間はUpToDateへ倒さない。desired versionのsource of truthはspec.versionである。
	kubernetesUpToDate := talos.NormalizeKubernetesVersion(upgradeState.observedVersion) == talos.NormalizeKubernetesVersion(cp.Spec.Version)
	upToDateReady := controlPlaneReady && desired > 0 && upToDate == desired && kubernetesUpToDate && !upgradeState.active && upgradeState.failureMessage == ""
	upToDateReason := "MachinesNotUpToDate"
	upToDateMessage := "Not all desired control-plane Machines report UpToDate."
	if !bootstrapState.workloadReady {
		upToDateReason = reasonWorkloadAPIUnavailable
		upToDateMessage = "The workload Kubernetes API is not ready yet."
	} else if !kubernetesUpToDate || upgradeState.active || upgradeState.failureMessage != "" {
		upToDateReason = "KubernetesVersionNotUpToDate"
		upToDateMessage = "The cluster has not converged to the desired Kubernetes version yet."
	} else if upToDateReady {
		upToDateReason = "UpToDate"
		upToDateMessage = "All desired control-plane Machines and the workload Kubernetes API are up to date."
	}
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneUpToDateCondition, conditionStatus(upToDateReady), upToDateReason, upToDateMessage, cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneRollingOutCondition, metav1.ConditionFalse, "NotRollingOut", "The control plane is not performing a rollout.", cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneScalingUpCondition, conditionStatus(actual < desired), scalingReason(actual < desired, "ScalingUp"), scalingMessage(actual < desired, "Control-plane Machines are being created.", "The desired control-plane replica count is satisfied."), cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneScalingDownCondition, conditionStatus(actual > desired), scalingReason(actual > desired, "ScalingDown"), scalingMessage(actual > desired, "Control-plane scale-down is waiting for quorum-safe etcd member removal.", "The desired control-plane replica count is not above the observed count."), cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneMachinesReadyCondition, conditionStatus(desired > 0 && ready == desired), machineReadinessReason(desired > 0 && ready == desired), machineReadinessMessage(desired > 0 && ready == desired), cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneMachinesUpToDateCondition, conditionStatus(desired > 0 && upToDate == desired), machineUpToDateReason(desired > 0 && upToDate == desired), machineUpToDateMessage(desired > 0 && upToDate == desired), cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneEtcdClusterAvailableCondition, conditionStatus(bootstrapState.etcdReady), bootstrapState.reason, bootstrapState.message, cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneCARotatingCondition, conditionStatus(caRotationState.active), caRotationState.reason, caRotationState.message, cp.Generation)
	cp.Status.KubernetesUpgrade = controlplanev1alpha1.TartControlPlaneKubernetesUpgradeStatus{
		TargetVersion:   upgradeState.targetVersion,
		ObservedVersion: upgradeState.observedVersion,
		FailureMessage:  upgradeState.failureMessage,
	}
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneKubernetesUpgradingCondition, conditionStatus(upgradeState.active), upgradeState.reason, upgradeState.message, cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneDeletingCondition, metav1.ConditionFalse, "NotDeleting", "The control plane is not being deleted.", cp.Generation)
	setPausedCondition(&cp.Status.Conditions, false, cp.Generation)
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

// controlPlaneCARotationStateはCA rotationの現在の観測結果を保持する。program counterではなく、reconcileのたびにTalosと bundle Secretの観測から再計算した値を運ぶだけの一時変数である。
type controlPlaneCARotationState struct {
	active       bool
	reason       string
	message      string
	requeueAfter time.Duration
}

// controlPlaneCARotationObservationは単一control-plane Machineについて観測したCA rotationの進行段階である。
type controlPlaneCARotationObservation struct {
	host        *infrav1alpha1.TartHost
	endpoint    string
	usedPending bool
	stage       controlplane.CATrustStage
}

// reconcileCARotationはTartCluster.spec.caRotationRequestedGenerationで要求されたCA rotationを、Talos公式の段階的CA更新手順(accepted CA追加→issuing CA切替→旧CA削除)に沿って進める。
// 進行段階はStatusのstep番号ではなく、毎回Pending/Active bundle Secretと各control-plane Machineの実際のTalos machine configurationから再計算するため、controller再起動後も安全に継続できる。
// observeControlPlaneCARotationは、削除中でない各control-plane MachineへTalos認証接続してCA trust stageを観測する。
// 途中で観測不能なMachineがあれば、rotationを一時停止するstateをhaltStateとして返す(errは返さずnilエラーで停止する既存の挙動を維持する)。
func (r *TartControlPlaneReconciler) observeControlPlaneCARotation(ctx context.Context, machines []clusterv1.Machine, activeBundle, pendingBundle *secrets.Bundle, activeCAs, pendingCAs controlplane.RotationCertificateAuthorities) ([]controlPlaneCARotationObservation, *controlPlaneCARotationState) {
	observations := make([]controlPlaneCARotationObservation, 0, len(machines))
	for index := range machines {
		machine := &machines[index]
		if !machine.DeletionTimestamp.IsZero() {
			continue
		}
		hostObject, _, err := r.observeMachineIdentity(ctx, machine)
		if err != nil {
			return nil, &controlPlaneCARotationState{active: true, reason: "MachineUnavailable", message: "A control-plane Machine could not be observed; CA rotation is paused.", requeueAfter: 30 * time.Second}
		}
		endpoint := hostTalosEndpoint(hostObject)
		talosClient, usedPending, err := r.dialForRotation(ctx, endpoint, activeBundle, pendingBundle)
		if err != nil {
			return nil, &controlPlaneCARotationState{active: true, reason: "MachineUnreachable", message: "A control-plane Machine Talos API could not be reached; CA rotation is paused.", requeueAfter: 30 * time.Second}
		}
		configuration, configErr := talosClient.ActiveMachineConfiguration(ctx)
		if closeErr := talosClient.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		if configErr != nil {
			return nil, &controlPlaneCARotationState{active: true, reason: "MachineConfigurationUnavailable", message: "A control-plane Machine active configuration could not be observed; CA rotation is paused.", requeueAfter: 30 * time.Second}
		}
		stage, stageErr := controlplane.ObserveCATrustStage(configuration, activeCAs, pendingCAs)
		if stageErr != nil || stage == controlplane.CATrustStageUnknown {
			return nil, &controlPlaneCARotationState{active: true, reason: reasonCAConfigurationUnrecognized, message: messageCAConfigurationUnrecognized}
		}
		observations = append(observations, controlPlaneCARotationObservation{host: hostObject, endpoint: endpoint, usedPending: usedPending, stage: stage})
	}
	return observations, nil
}

func (r *TartControlPlaneReconciler) reconcileCARotation(ctx context.Context, cluster *infrav1alpha1.TartCluster, machines []clusterv1.Machine, scaleDownPending bool) (controlPlaneCARotationState, error) {
	notRequested := controlPlaneCARotationState{reason: "NotRequested", message: "No CA rotation has been requested."}
	if cluster == nil || cluster.Spec.CARotationRequestedGeneration == nil {
		return notRequested, nil
	}
	if scaleDownPending {
		return controlPlaneCARotationState{active: true, reason: "ScaleDownInProgress", message: "CA rotation is paused while a control-plane scale-down is in progress.", requeueAfter: controlPlaneScaleDownRequeue}, nil
	}
	clusterID, err := parseClusterID(cluster.Spec.ClusterID)
	if err != nil {
		return notRequested, nil //nolint:nilerr // cluster identityが未確定な段階はgetTartClusterで既に停止しているため、ここでは静かに何もしない。
	}
	target, err := controlplane.NextGeneration(cluster.Status.ActiveSecretGeneration)
	if err != nil {
		return controlPlaneCARotationState{active: true, reason: "RotationGenerationInvalid", message: "The next CA rotation secret bundle generation is invalid."}, err
	}
	if *cluster.Spec.CARotationRequestedGeneration != target {
		return notRequested, nil
	}

	activeBundle, activeCAs, err := r.observeRotationBundle(ctx, cluster, clusterID, cluster.Status.ActiveSecretGeneration, controlplane.BundleStateActive)
	if err != nil {
		return controlPlaneCARotationState{active: true, reason: "ActiveBundleUnavailable", message: "The active cluster secret bundle could not be observed for CA rotation.", requeueAfter: 30 * time.Second}, nil //nolint:nilerr
	}
	pendingBundle, pendingCAs, err := r.observeRotationBundle(ctx, cluster, clusterID, target, controlplane.BundleStatePending)
	if err != nil {
		return controlPlaneCARotationState{active: true, reason: "PendingBundleUnavailable", message: "The next-generation Pending cluster secret bundle is not available yet.", requeueAfter: 30 * time.Second}, nil //nolint:nilerr
	}

	observations, haltState := r.observeControlPlaneCARotation(ctx, machines, activeBundle, pendingBundle, activeCAs, pendingCAs)
	if haltState != nil {
		return *haltState, nil
	}
	if len(observations) == 0 {
		return controlPlaneCARotationState{active: true, reason: "MachineUnavailable", message: "No control-plane Machine is available yet for CA rotation.", requeueAfter: 30 * time.Second}, nil
	}

	minStage := observations[0].stage
	for _, observation := range observations[1:] {
		if observation.stage < minStage {
			minStage = observation.stage
		}
	}

	switch minStage {
	case controlplane.CATrustStageStable:
		for _, observation := range observations {
			if observation.stage != controlplane.CATrustStageStable {
				continue
			}
			if err := r.advanceCATrust(ctx, observation, activeCAs, pendingCAs, activeBundle, pendingBundle, false); err != nil {
				return controlPlaneCARotationState{active: true, reason: reasonCATrustUpdateFailed, message: "Adding the next-generation certificate authorities to a control-plane Machine failed.", requeueAfter: 15 * time.Second}, err
			}
		}
		return controlPlaneCARotationState{active: true, reason: "AddingAcceptedCA", message: "CA rotation is adding the next-generation certificate authorities as accepted CAs on every control-plane Machine.", requeueAfter: 15 * time.Second}, nil
	case controlplane.CATrustStageDualTrust:
		for _, observation := range observations {
			if observation.stage != controlplane.CATrustStageDualTrust {
				continue
			}
			if err := r.advanceCATrust(ctx, observation, activeCAs, pendingCAs, activeBundle, pendingBundle, true); err != nil {
				return controlPlaneCARotationState{active: true, reason: reasonCATrustUpdateFailed, message: "Switching the issuing certificate authority on a control-plane Machine failed.", requeueAfter: 15 * time.Second}, err
			}
		}
		return controlPlaneCARotationState{active: true, reason: "SwitchingIssuingCA", message: "CA rotation is switching the issuing certificate authority to the next generation on every control-plane Machine.", requeueAfter: 15 * time.Second}, nil
	case controlplane.CATrustStageCutover:
		for _, observation := range observations {
			if observation.stage != controlplane.CATrustStageCutover {
				continue
			}
			if err := r.finalizeCATrust(ctx, observation, activeBundle, pendingBundle, pendingCAs); err != nil {
				return controlPlaneCARotationState{active: true, reason: reasonCATrustUpdateFailed, message: "Removing the previous-generation certificate authority from a control-plane Machine failed.", requeueAfter: 15 * time.Second}, err
			}
		}
		return controlPlaneCARotationState{active: true, reason: "RemovingAcceptedCA", message: "CA rotation is removing the previous-generation certificate authority from every control-plane Machine.", requeueAfter: 15 * time.Second}, nil
	case controlplane.CATrustStageRotated:
		if err := r.promoteCARotation(ctx, cluster, clusterID, target); err != nil {
			return controlPlaneCARotationState{active: true, reason: "CARotationPromotionFailed", message: "CA rotation completed on every control-plane Machine, but promoting the active secret bundle generation failed.", requeueAfter: 15 * time.Second}, err
		}
		return controlPlaneCARotationState{reason: "Completed", message: "CA rotation completed; the active cluster secret bundle generation was promoted."}, nil
	case controlplane.CATrustStageUnknown:
		return controlPlaneCARotationState{active: true, reason: reasonCAConfigurationUnrecognized, message: messageCAConfigurationUnrecognized}, nil
	default:
		return controlPlaneCARotationState{active: true, reason: reasonCAConfigurationUnrecognized, message: messageCAConfigurationUnrecognized}, nil
	}
}

// observeRotationBundleは指定したgenerationとstateのbundle Secretを取得し、契約を検証してrotation対象CAを取り出す。
func (r *TartControlPlaneReconciler) observeRotationBundle(ctx context.Context, cluster *infrav1alpha1.TartCluster, clusterID clusterdomain.ClusterID, generation int32, state string) (*secrets.Bundle, controlplane.RotationCertificateAuthorities, error) {
	name, err := controlplane.BundleName(cluster.Name, clusterID, generation)
	if err != nil {
		return nil, controlplane.RotationCertificateAuthorities{}, err
	}
	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, &secret); err != nil {
		return nil, controlplane.RotationCertificateAuthorities{}, err
	}
	if err := controlplane.ValidateBundleSecretContract(&secret, cluster.Namespace, cluster.Name, clusterID, generation, state, cluster.UID); err != nil {
		return nil, controlplane.RotationCertificateAuthorities{}, err
	}
	bundle, err := controlplane.DecodeBundleData(secret.Data, clusterID)
	if err != nil {
		return nil, controlplane.RotationCertificateAuthorities{}, err
	}
	cas, err := controlplane.ExtractRotationCertificateAuthorities(bundle)
	if err != nil {
		return nil, controlplane.RotationCertificateAuthorities{}, err
	}
	return bundle, cas, nil
}

// dialForRotationはactive bundleの信頼情報から先に接続を試み、失敗した場合はpending bundleで再試行する。どちらのgenerationがnodeの現在のissuing CAとして実際に検証できたかを呼び出し側へ返す。
func (r *TartControlPlaneReconciler) dialForRotation(ctx context.Context, endpoint string, active, pending *secrets.Bundle) (*talos.Client, bool, error) {
	if talosClient, err := r.dialRotationBundle(ctx, endpoint, active); err == nil {
		return talosClient, false, nil
	}
	talosClient, err := r.dialRotationBundle(ctx, endpoint, pending)
	if err != nil {
		return nil, false, err
	}
	return talosClient, true, nil
}

func (r *TartControlPlaneReconciler) dialRotationBundle(ctx context.Context, endpoint string, bundle *secrets.Bundle) (*talos.Client, error) {
	connectionContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return talos.DialAuthenticatedFromBundle(connectionContext, endpoint, bundle)
}

func (r *TartControlPlaneReconciler) dialObservedRotationBundle(ctx context.Context, observation controlPlaneCARotationObservation, activeBundle, pendingBundle *secrets.Bundle) (*talos.Client, error) {
	bundle := activeBundle
	if observation.usedPending {
		bundle = pendingBundle
	}
	return r.dialRotationBundle(ctx, observation.endpoint, bundle)
}

// advanceCATrustはstage Stableのnodeへ次generationのCAをaccepted CAとして追加し(cutover=false)、stage DualTrustのnodeへissuing CAを次generationへ切替える(cutover=true)。いずれもTalosのconfig applyへ委譲し、再起動なしでcertificateだけを更新する。
func (r *TartControlPlaneReconciler) advanceCATrust(ctx context.Context, observation controlPlaneCARotationObservation, active, pending controlplane.RotationCertificateAuthorities, activeBundle, pendingBundle *secrets.Bundle, cutover bool) error {
	talosClient, err := r.dialObservedRotationBundle(ctx, observation, activeBundle, pendingBundle)
	if err != nil {
		return err
	}
	configuration, err := talosClient.ActiveMachineConfiguration(ctx)
	if err != nil {
		if closeErr := talosClient.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		return err
	}

	issuingMachine, issuingAPI, issuingAggregator := active.Machine, active.KubernetesAPI, active.KubernetesAggregator
	acceptedMachine, acceptedAPI, acceptedAggregator := pending.Machine, pending.KubernetesAPI, pending.KubernetesAggregator
	if cutover {
		issuingMachine, issuingAPI, issuingAggregator = pending.Machine, pending.KubernetesAPI, pending.KubernetesAggregator
		acceptedMachine, acceptedAPI, acceptedAggregator = active.Machine, active.KubernetesAPI, active.KubernetesAggregator
	}

	updated, err := talos.SetMachineCertificateAuthority(configuration, issuingMachine, acceptedMachine)
	if err == nil {
		updated, err = talos.SetKubernetesAPICertificateAuthority(updated, issuingAPI, acceptedAPI)
	}
	if err == nil {
		updated, err = talos.SetKubernetesAggregatorCertificateAuthority(updated, issuingAggregator, acceptedAggregator)
	}
	if err != nil {
		if closeErr := talosClient.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		return err
	}
	applyErr := talosClient.ApplyConfiguration(ctx, updated)
	if closeErr := talosClient.Close(); closeErr != nil && applyErr == nil {
		return closeErr
	}
	return applyErr
}

// finalizeCATrustはstage Cutoverのnodeから旧generationのCAをaccepted CAから外し、issuing CAだけを信頼する最終状態にする。Talos公式のCA rotation手順の最終段階(旧CA削除)にあたる。
func (r *TartControlPlaneReconciler) finalizeCATrust(ctx context.Context, observation controlPlaneCARotationObservation, activeBundle, pendingBundle *secrets.Bundle, pending controlplane.RotationCertificateAuthorities) error {
	talosClient, err := r.dialObservedRotationBundle(ctx, observation, activeBundle, pendingBundle)
	if err != nil {
		return err
	}
	configuration, err := talosClient.ActiveMachineConfiguration(ctx)
	if err != nil {
		if closeErr := talosClient.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		return err
	}
	updated, err := talos.SetMachineCertificateAuthority(configuration, pending.Machine)
	if err == nil {
		updated, err = talos.SetKubernetesAPICertificateAuthority(updated, pending.KubernetesAPI)
	}
	if err == nil {
		updated, err = talos.SetKubernetesAggregatorCertificateAuthority(updated, pending.KubernetesAggregator)
	}
	if err != nil {
		if closeErr := talosClient.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		return err
	}
	applyErr := talosClient.ApplyConfiguration(ctx, updated)
	if closeErr := talosClient.Close(); closeErr != nil && applyErr == nil {
		return closeErr
	}
	return applyErr
}

// promoteCARotationはPending bundle Secretのlabelをactiveへ書き換え(dataはimmutableなまま維持する)、TartCluster.status.activeSecretGenerationを新generationへ進める。この呼び出しの直前に全control-plane Machineが新generationのCAだけを信頼していることを確認済みである。
func (r *TartControlPlaneReconciler) promoteCARotation(ctx context.Context, cluster *infrav1alpha1.TartCluster, clusterID clusterdomain.ClusterID, target int32) error {
	name, err := controlplane.BundleName(cluster.Name, clusterID, target)
	if err != nil {
		return err
	}
	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, &secret); err != nil {
		return err
	}
	if secret.Labels[controlplane.BundleStateLabel] == controlplane.BundleStateActive {
		// 既に他のreconcileで昇格済みである。
	} else {
		original := secret.DeepCopy()
		secret.Labels = maps.Clone(secret.Labels)
		secret.Labels[controlplane.BundleStateLabel] = controlplane.BundleStateActive
		if err := r.Patch(ctx, &secret, client.MergeFrom(original)); err != nil {
			return err
		}
	}
	if cluster.Status.ActiveSecretGeneration == target {
		return nil
	}
	original := cluster.DeepCopy()
	cluster.Status.ActiveSecretGeneration = target
	return r.Status().Patch(ctx, cluster, client.MergeFrom(original))
}

func (r *TartControlPlaneReconciler) enqueueAllControlPlanes(ctx context.Context, _ client.Object) []reconcile.Request {
	var list controlplanev1alpha1.TartControlPlaneList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
	}
	return requests
}

func (r *TartControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlplanev1alpha1.TartControlPlane{}).
		Owns(&clusterv1.Machine{}).
		Watches(&clusterv1.Cluster{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Watches(&infrav1alpha1.TartMachine{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Watches(&bootstrapv1alpha1.TartBootstrapConfig{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Watches(&infrav1alpha1.TartCluster{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Watches(&infrav1alpha1.TartMachineTemplate{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Watches(&bootstrapv1alpha1.TartBootstrapConfigTemplate{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Named("tartcontrolplane").
		Complete(r)
}
