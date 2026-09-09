package tartcontrolplane

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controller"
	"github.com/walnuts1018/cluster-api-provider-tart/controller/tartmachine"
	domaincontrolplane "github.com/walnuts1018/cluster-api-provider-tart/domain/controlplane"
)

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
		if target.Annotations[controlPlaneEtcdDeleteHook] != controller.ControlPlaneEtcdDeleteHookValue {
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
	if machine.Annotations[controlPlaneEtcdDeleteHook] == controller.ControlPlaneEtcdDeleteHookValue {
		return nil
	}
	original := machine.DeepCopy()
	machine.Annotations = maps.Clone(machine.Annotations)
	if machine.Annotations == nil {
		machine.Annotations = make(map[string]string)
	}
	machine.Annotations[controlPlaneEtcdDeleteHook] = controller.ControlPlaneEtcdDeleteHookValue
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
	// annotationが既に存在する場合はreconcileAnnotatedEtcdMemberがこのreconcileの結論を確定させる。
	// handledではなくannotatedで分岐しないと、削除hookの解除に成功した直後(handled=false)に
	// 以降のannotation未設定パスへ誤って進み、既にetcdから除去済みのmemberへ対して不要な
	// Talos APIの再観測が発生する。
	if annotated || err != nil {
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
		return true, &scaleDownPendingError{reason: "EtcdSurvivorUnavailable", message: "A surviving control-plane member could not be observed.", cause: err}
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
	return domaincontrolplane.CanRemoveMember(domaincontrolplane.RemovalObservation{
		MemberCount:          len(members),
		HealthyMemberCount:   healthyMembers,
		TargetHealthy:        etcdStatusHealthy(targetObservation.status),
		TargetHealthObserved: true,
	})
}

func (r *TartControlPlaneReconciler) removeObservedEtcdMember(ctx context.Context, survivor *etcdMachineObservation, memberID uint64) (bool, error) {
	removalClient, err := r.dialAuthenticated(ctx, survivor.host, survivor.config)
	if err != nil {
		return true, &scaleDownPendingError{reason: "EtcdSurvivorUnavailable", message: "A surviving control-plane member could not be authenticated for etcd removal.", cause: err}
	}
	removeErr := removalClient.RemoveEtcdMember(ctx, memberID)
	if closeErr := removalClient.Close(); closeErr != nil && removeErr == nil {
		return true, closeErr
	}
	if removeErr != nil {
		return true, &scaleDownPendingError{reason: "EtcdMemberRemovalFailed", message: "The etcd member removal request did not complete successfully.", cause: removeErr}
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
		return annotatedID, true, true, &scaleDownPendingError{reason: "EtcdObservationTransient", message: "Etcd membership observation is transient while the deleting Machine is waiting.", cause: err}
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
	if ref.APIGroup != infrav1alpha1.GroupVersion.Group || ref.Kind != controller.TartMachineKind || ref.Name == "" {
		return nil, nil, errors.New("control-plane Machine infrastructure reference is invalid")
	}
	provider := &infrav1alpha1.TartMachine{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: ref.Name}, provider); err != nil {
		return nil, nil, err
	}
	if err := controller.ValidateProviderOwner(provider, machine, clusterv1.GroupVersion.String(), controller.CAPIMachineKind); err != nil {
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
	endpoint := controller.HostTalosEndpoint(hostObject)
	if endpoint == "" {
		return nil, nil, errors.New("control-plane Host Talos endpoint is unavailable")
	}
	configuration, err := (&tartmachine.TartMachineReconciler{Client: r.Client}).BootstrapConfiguration(ctx, provider)
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
	return talos.DialAuthenticatedFromConfiguration(connectionContext, controller.HostTalosEndpoint(hostObject), configuration)
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
