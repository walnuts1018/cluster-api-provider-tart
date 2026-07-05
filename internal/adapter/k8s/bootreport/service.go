// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bootreport

import (
	"context"
	"errors"
	"fmt"
	"math"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	bootreportdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/bootreport"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

var (
	ErrOperationNotFound = errors.New("operation or plan not found")
	ErrReportConflict    = errors.New("boot report conflicts with operation state")
)

type PlanProvider interface {
	GetPlan(context.Context, client.ObjectKey) (agentprotocol.SignedPlan, error)
}

type Service struct {
	client client.Client
	plans  PlanProvider
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthostoperations/status,verbs=get;update;patch

func NewService(k8sClient client.Client, plans PlanProvider) *Service {
	return &Service{client: k8sClient, plans: plans}
}

func (service *Service) ReportBoot(
	ctx context.Context,
	key client.ObjectKey,
	request agentprotocol.BootReportRequest,
	now metav1.Time,
) error {
	expected, err := service.expectedBoot(ctx, key, request.PlanDigest)
	if err != nil {
		return err
	}

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		operation := &infrastructurev1beta1.TartHostOperation{}
		if err := service.client.Get(ctx, key, operation); err != nil {
			if apierrors.IsNotFound(err) {
				return ErrOperationNotFound
			}
			return err
		}
		if operation.Spec.OperationID != request.OperationUID ||
			operation.Spec.PlanDigest != request.PlanDigest {
			return ErrOperationNotFound
		}
		phase, err := operationdomain.ParsePhase(string(operation.Status.Phase))
		if err != nil {
			return fmt.Errorf("%w: %v", ErrReportConflict, err)
		}
		result, err := bootreportdomain.Evaluate(
			phase,
			statusReport(operation.Status.LastBootReport),
			requestReport(request),
			expected,
		)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrReportConflict, err)
		}
		switch result.Decision {
		case bootreportdomain.DecisionDuplicate:
			return nil
		case bootreportdomain.DecisionRecorded, bootreportdomain.DecisionCompleted:
			operation.Status.LastBootReport = &infrastructurev1beta1.BootReportStatus{
				BootID:             request.BootID,
				ActiveSlot:         infrastructurev1beta1.OSSlot(request.ActiveSlot),
				ArtifactGeneration: int64(request.ArtifactGeneration),
				StateMounted:       request.StateMounted,
				DataMounted:        request.DataMounted,
				BootstrapApplied:   request.BootstrapApplied,
				ReportedAt:         now,
			}
			operation.Status.Phase = infrastructurev1beta1.TartHostOperationPhase(result.NextPhase)
			return service.client.Status().Update(ctx, operation)
		}
		return fmt.Errorf("unsupported boot report decision: %q", result.Decision)
	})
	if err != nil {
		return fmt.Errorf("record boot report: %w", err)
	}
	return nil
}

func (service *Service) expectedBoot(
	ctx context.Context,
	key client.ObjectKey,
	planDigest string,
) (bootreportdomain.ExpectedBoot, error) {
	signedPlan, err := service.plans.GetPlan(ctx, key)
	if err != nil {
		return bootreportdomain.ExpectedBoot{}, fmt.Errorf("%w: load signed plan", ErrOperationNotFound)
	}
	validated, err := agentprotocol.ValidatePlan(signedPlan.Plan)
	if err != nil {
		return bootreportdomain.ExpectedBoot{}, fmt.Errorf("%w: invalid signed plan", ErrOperationNotFound)
	}
	digest, err := validated.Digest()
	if err != nil || digest.String() != planDigest {
		return bootreportdomain.ExpectedBoot{}, fmt.Errorf("%w: plan digest mismatch", ErrOperationNotFound)
	}
	slot, err := targetSlot(signedPlan.Plan.AllowedTargetRoles)
	if err != nil || signedPlan.Plan.Artifact.Generation > math.MaxInt64 {
		return bootreportdomain.ExpectedBoot{}, fmt.Errorf("%w: invalid boot target", ErrOperationNotFound)
	}
	return bootreportdomain.ExpectedBoot{
		ActiveSlot:         slot,
		ArtifactGeneration: signedPlan.Plan.Artifact.Generation,
	}, nil
}

func targetSlot(roles []agentprotocol.DiskRole) (string, error) {
	var slot string
	for _, role := range roles {
		var candidate string
		switch role {
		case agentprotocol.DiskRoleOSA:
			candidate = "A"
		case agentprotocol.DiskRoleOSB:
			candidate = "B"
		case agentprotocol.DiskRoleBoot,
			agentprotocol.DiskRoleVerityA,
			agentprotocol.DiskRoleVerityB,
			agentprotocol.DiskRoleState,
			agentprotocol.DiskRoleData:
			continue
		}
		if slot != "" && slot != candidate {
			return "", errors.New("plan targets more than one OS slot")
		}
		slot = candidate
	}
	if slot == "" {
		return "", errors.New("plan does not target an OS slot")
	}
	return slot, nil
}

func requestReport(request agentprotocol.BootReportRequest) bootreportdomain.Report {
	return bootreportdomain.Report{
		BootID:             request.BootID,
		ActiveSlot:         request.ActiveSlot,
		ArtifactGeneration: request.ArtifactGeneration,
		StateMounted:       request.StateMounted,
		DataMounted:        request.DataMounted,
		BootstrapApplied:   request.BootstrapApplied,
	}
}

func statusReport(status *infrastructurev1beta1.BootReportStatus) *bootreportdomain.Report {
	if status == nil || status.ArtifactGeneration < 0 {
		return nil
	}
	return &bootreportdomain.Report{
		BootID:             status.BootID,
		ActiveSlot:         string(status.ActiveSlot),
		ArtifactGeneration: uint64(status.ArtifactGeneration),
		StateMounted:       status.StateMounted,
		DataMounted:        status.DataMounted,
		BootstrapApplied:   status.BootstrapApplied,
	}
}
