package controller

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capiannotations "sigs.k8s.io/cluster-api/util/annotations"

	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
)

func SetCondition(conditions *[]metav1.Condition, conditionType string, status metav1.ConditionStatus, reason, message string, generation int64) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
	})
}

func IsPaused(object metav1.Object) bool {
	return capiannotations.HasPaused(object)
}

func SetPausedCondition(conditions *[]metav1.Condition, paused bool, generation int64) {
	status := metav1.ConditionFalse
	reason := "NotPaused"
	message := "Reconciliation is not paused."
	if paused {
		status = metav1.ConditionTrue
		reason = "Paused"
		message = "Reconciliation is paused by the cluster.x-k8s.io/paused annotation."
	}
	SetCondition(conditions, controlplanev1alpha1.TartControlPlanePausedCondition, status, reason, message, generation)
}
