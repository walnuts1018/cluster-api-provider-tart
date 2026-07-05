package provisioningagent

import (
	"context"
	"fmt"

	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/disk"
	agentplan "github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/plan"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

type TargetWriter interface {
	WriteTargets(context.Context, agentprotocol.ValidatedPlan, disk.Device) error
}

type Service struct {
	writer TargetWriter
}

func NewService(writer TargetWriter) *Service {
	return &Service{writer: writer}
}

// ExecuteはPlanとdisk identityを検証し終えるまで、破壊的なWriterを呼び出さない。
func (service *Service) Execute(
	ctx context.Context,
	plan agentprotocol.ValidatedPlan,
	devices []disk.Device,
) error {
	if err := agentplan.ValidateTargets(plan); err != nil {
		return fmt.Errorf("validate Plan targets: %w", err)
	}
	target, err := disk.Select(plan.Value().RootDevice, devices)
	if err != nil {
		return fmt.Errorf("select root disk: %w", err)
	}
	if err := service.writer.WriteTargets(ctx, plan, target); err != nil {
		return fmt.Errorf("write Plan targets: %w", err)
	}
	return nil
}
