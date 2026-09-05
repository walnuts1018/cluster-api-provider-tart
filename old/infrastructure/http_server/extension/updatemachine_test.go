package extension

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func TestUpdateMachineHandlerはOperationPhaseをRetryResponseへ写像する(t *testing.T) {
	tests := []struct {
		name       string
		phase      infrastructurev1beta1.TartHostOperationPhase
		status     runtimehooksv1.ResponseStatus
		retryAfter int32
	}{
		{name: "not reconciled", phase: "", status: runtimehooksv1.ResponseStatusSuccess, retryAfter: 10},
		{name: "pending", phase: infrastructurev1beta1.TartHostOperationPhasePending, status: runtimehooksv1.ResponseStatusSuccess, retryAfter: 10},
		{name: "boot trial", phase: infrastructurev1beta1.TartHostOperationPhaseBootTrial, status: runtimehooksv1.ResponseStatusSuccess, retryAfter: 10},
		{name: "rolling back", phase: infrastructurev1beta1.TartHostOperationPhaseRollingBack, status: runtimehooksv1.ResponseStatusSuccess, retryAfter: 10},
		{name: "succeeded", phase: infrastructurev1beta1.TartHostOperationPhaseSucceeded, status: runtimehooksv1.ResponseStatusSuccess},
		{name: "failed", phase: infrastructurev1beta1.TartHostOperationPhaseFailed, status: runtimehooksv1.ResponseStatusFailure},
		{name: "recovery required", phase: infrastructurev1beta1.TartHostOperationPhaseRecoveryRequired, status: runtimehooksv1.ResponseStatusFailure},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewUpdateMachineHandler(staticUpdateStarter{
				operation: &infrastructurev1beta1.TartHostOperation{
					Status: infrastructurev1beta1.TartHostOperationStatus{Phase: test.phase},
				},
			})
			response := &runtimehooksv1.UpdateMachineResponse{}

			handler.Handle(t.Context(), &runtimehooksv1.UpdateMachineRequest{}, response)

			if response.Status != test.status {
				t.Fatalf("Status = %q, want %q", response.Status, test.status)
			}
			if response.RetryAfterSeconds != test.retryAfter {
				t.Fatalf("RetryAfterSeconds = %d, want %d", response.RetryAfterSeconds, test.retryAfter)
			}
		})
	}
}

func TestUpdateMachineHandlerは開始失敗をFailureにする(t *testing.T) {
	handler := NewUpdateMachineHandler(staticUpdateStarter{err: errors.New("registry unavailable")})
	response := &runtimehooksv1.UpdateMachineResponse{}

	handler.Handle(t.Context(), &runtimehooksv1.UpdateMachineRequest{}, response)

	if response.Status != runtimehooksv1.ResponseStatusFailure {
		t.Fatalf("Status = %q, want Failure", response.Status)
	}
	if response.Message == "" {
		t.Fatal("Message is empty")
	}
}

func TestUpdateMachineHandlerは無効な対象で開始しない(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*runtimehooksv1.UpdateMachineRequest)
		gates  UpdateTargetFeatureGates
	}{
		{
			name:  "worker gate無効",
			gates: UpdateTargetFeatureGates{},
		},
		{
			name: "single control plane gate無効",
			mutate: func(request *runtimehooksv1.UpdateMachineRequest) {
				markControlPlaneMachine(&request.Desired.Machine)
			},
			gates: UpdateTargetFeatureGates{Worker: true, MultiControlPlane: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			starter := &countingUpdateStarter{
				operation: &infrastructurev1beta1.TartHostOperation{
					Status: infrastructurev1beta1.TartHostOperationStatus{
						Phase: infrastructurev1beta1.TartHostOperationPhasePending,
					},
				},
			}
			handler := NewUpdateMachineHandlerWithSupport(
				starter,
				newTestTargetSupportChecker(t, test.gates, NodeLifecycleFeatureGates{}),
			)
			request := updateMachineRequest()
			if test.mutate != nil {
				test.mutate(request)
			}
			response := &runtimehooksv1.UpdateMachineResponse{}

			handler.Handle(t.Context(), request, response)

			if response.Status != runtimehooksv1.ResponseStatusFailure {
				t.Fatalf("Status = %q, want %q", response.Status, runtimehooksv1.ResponseStatusFailure)
			}
			if starter.calls != 0 {
				t.Fatalf("Start() calls = %d, want 0", starter.calls)
			}
		})
	}
}

func TestTargetSupportCheckerはsingleControlPlaneNodeLifecycleをExperimental理由で拒否する(t *testing.T) {
	machine := controlPlaneMachine("cp-1")
	checker := newTestTargetSupportChecker(
		t,
		UpdateTargetFeatureGates{Worker: true, MultiControlPlane: true, SingleControlPlane: true},
		NodeLifecycleFeatureGates{Worker: true, MultiControlPlane: true},
		machine,
	)

	supported, reason, err := checker.SupportsNodeLifecycleMachine(t.Context(), machine)
	if err != nil {
		t.Fatalf("SupportsNodeLifecycleMachine() error = %v", err)
	}
	if supported {
		t.Fatalf("SupportsNodeLifecycleMachine() = true, want false; reason=%q", reason)
	}
	assertContainsAll(t, reason,
		"single control plane",
		"Experimental",
		"management API outage E2E pending",
	)
}

type staticUpdateStarter struct {
	operation *infrastructurev1beta1.TartHostOperation
	err       error
}

func (starter staticUpdateStarter) Start(
	context.Context,
	*runtimehooksv1.UpdateMachineRequest,
) (*infrastructurev1beta1.TartHostOperation, error) {
	return starter.operation, starter.err
}

type countingUpdateStarter struct {
	operation *infrastructurev1beta1.TartHostOperation
	calls     int
}

func (starter *countingUpdateStarter) Start(
	context.Context,
	*runtimehooksv1.UpdateMachineRequest,
) (*infrastructurev1beta1.TartHostOperation, error) {
	starter.calls++
	return starter.operation, nil
}

func updateMachineRequest() *runtimehooksv1.UpdateMachineRequest {
	return &runtimehooksv1.UpdateMachineRequest{
		Desired: runtimehooksv1.UpdateMachineRequestObjects{
			Machine: clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "machine-1",
					Namespace: "default",
					Labels: map[string]string{
						clusterv1.ClusterNameLabel: "sample",
					},
				},
				Spec: clusterv1.MachineSpec{
					ClusterName: "sample",
				},
			},
		},
	}
}
