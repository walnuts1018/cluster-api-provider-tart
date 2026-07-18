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

package provisioningagent

import (
	"context"
	"fmt"

	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
	"github.com/walnuts1018/cluster-api-provider-tart/infrastructure/provisioning_agent/disk"
	agentplan "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/provisioning_agent/plan"
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
