package controller

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capiannotations "sigs.k8s.io/cluster-api/util/annotations"

	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// setNotImplemented reports a resource as safely stopped because the reconcile logic
// for its Condition type has not been implemented yet. It never performs an external
// side effect, so it is always safe to report while the real policy is still being
// built out in later sessions. See docs/development/decisions.md.
func setNotImplemented(conditions *[]metav1.Condition, conditionType string, generation int64) {
	setCondition(conditions, conditionType, metav1.ConditionFalse, infrav1alpha1.ReasonNotImplemented, "Reconcile logic for this resource is not implemented yet; no external side effect has been attempted.", generation)
}

func setCondition(conditions *[]metav1.Condition, conditionType string, status metav1.ConditionStatus, reason, message string, generation int64) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
	})
}

func isPaused(object metav1.Object) bool {
	return capiannotations.HasPaused(object)
}

func setPausedCondition(conditions *[]metav1.Condition, paused bool, generation int64) {
	status := metav1.ConditionFalse
	reason := "NotPaused"
	message := "Reconciliation is not paused."
	if paused {
		status = metav1.ConditionTrue
		reason = "Paused"
		message = "Reconciliation is paused by the cluster.x-k8s.io/paused annotation."
	}
	setCondition(conditions, controlplanev1alpha1.TartControlPlanePausedCondition, status, reason, message, generation)
}
