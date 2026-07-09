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

package nodelifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/opencontainers/go-digest"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

const APIVersion = "infrastructure.cluster.x-k8s.io/nodelifecycle/v1"

var identifierPattern = regexp.MustCompile(`^[a-zA-Z0-9][-a-zA-Z0-9_.:]{0,127}$`)

// PlanはNode Lifecycle Serviceが受け取る署名対象のJSON payloadである。
type Plan struct {
	APIVersion     string             `json:"apiVersion"`
	OperationID    string             `json:"operationID"`
	CurrentVersion string             `json:"currentVersion"`
	TargetVersion  string             `json:"targetVersion"`
	UpdateClass    domain.UpdateClass `json:"updateClass"`
	NodeRole       domain.NodeRole    `json:"nodeRole"`
	SnapshotRef    string             `json:"snapshotRef,omitempty"`
	Deadline       time.Time          `json:"deadline"`
	Steps          []domain.Step      `json:"steps"`
}

type SignedPlan struct {
	Plan      Plan                    `json:"plan"`
	Signature agentprotocol.Signature `json:"signature"`
}

type ValidatedPlan struct {
	plan Plan
}

func ValidatePlan(plan Plan) (ValidatedPlan, error) {
	switch {
	case plan.APIVersion != APIVersion:
		return ValidatedPlan{}, fmt.Errorf("unsupported apiVersion: %q", plan.APIVersion)
	case !identifierPattern.MatchString(plan.OperationID):
		return ValidatedPlan{}, errors.New("operationID is invalid")
	case plan.Deadline.IsZero():
		return ValidatedPlan{}, errors.New("deadline is required")
	}
	domainPlan, err := ToDomainPlan(plan)
	if err != nil {
		return ValidatedPlan{}, err
	}
	if len(domainPlan.Steps) == 0 {
		return ValidatedPlan{}, errors.New("steps must not be empty")
	}
	seen := make(map[domain.Step]struct{}, len(domainPlan.Steps))
	for _, step := range domainPlan.Steps {
		if _, exists := seen[step]; exists {
			return ValidatedPlan{}, fmt.Errorf("duplicate lifecycle step: %q", step)
		}
		seen[step] = struct{}{}
	}
	return ValidatedPlan{plan: plan}, nil
}

func FromDomainPlan(plan domain.Plan, deadline time.Time) (ValidatedPlan, error) {
	return ValidatePlan(Plan{
		APIVersion:     APIVersion,
		OperationID:    plan.OperationID,
		CurrentVersion: plan.CurrentVersion,
		TargetVersion:  plan.TargetVersion,
		UpdateClass:    plan.UpdateClass,
		NodeRole:       plan.NodeRole,
		SnapshotRef:    plan.SnapshotRef,
		Deadline:       deadline.UTC(),
		Steps:          append([]domain.Step(nil), plan.Steps...),
	})
}

func ToDomainPlan(plan Plan) (domain.Plan, error) {
	domainPlan := domain.Plan{
		OperationID:    plan.OperationID,
		CurrentVersion: plan.CurrentVersion,
		TargetVersion:  plan.TargetVersion,
		UpdateClass:    plan.UpdateClass,
		NodeRole:       plan.NodeRole,
		SnapshotRef:    plan.SnapshotRef,
		Steps:          append([]domain.Step(nil), plan.Steps...),
	}
	switch plan.UpdateClass {
	case domain.UpdateClassKubernetesBinary, domain.UpdateClassStateMigration:
	default:
		return domain.Plan{}, fmt.Errorf("unsupported updateClass: %q", plan.UpdateClass)
	}
	switch plan.NodeRole {
	case domain.NodeRoleWorker, domain.NodeRoleControlPlane:
	default:
		return domain.Plan{}, fmt.Errorf("unsupported nodeRole: %q", plan.NodeRole)
	}
	preflight := domain.PreflightInput{
		CurrentVersion: plan.CurrentVersion,
		TargetVersion:  plan.TargetVersion,
		UpdateClass:    plan.UpdateClass,
		NodeRole:       plan.NodeRole,
		SnapshotRef:    plan.SnapshotRef,
	}
	if err := domain.Preflight(preflight); err != nil {
		return domain.Plan{}, err
	}
	for _, step := range plan.Steps {
		if !knownStep(step) {
			return domain.Plan{}, fmt.Errorf("unknown lifecycle step %q", step)
		}
	}
	return domainPlan, nil
}

func (plan ValidatedPlan) Value() Plan {
	return plan.plan
}

func (plan ValidatedPlan) DomainPlan() (domain.Plan, error) {
	return ToDomainPlan(plan.plan)
}

func (plan ValidatedPlan) CanonicalJSON() ([]byte, error) {
	data, err := json.Marshal(plan.plan)
	if err != nil {
		return nil, fmt.Errorf("marshal lifecycle plan: %w", err)
	}
	canonical, err := jsoncanonicalizer.Transform(data)
	if err != nil {
		return nil, fmt.Errorf("canonicalize lifecycle plan: %w", err)
	}
	return canonical, nil
}

func (plan ValidatedPlan) Digest() (digest.Digest, error) {
	canonical, err := plan.CanonicalJSON()
	if err != nil {
		return "", err
	}
	return digest.FromBytes(canonical), nil
}

func knownStep(step domain.Step) bool {
	for _, candidate := range domain.LifecycleSteps() {
		if candidate == step {
			return true
		}
	}
	return false
}
