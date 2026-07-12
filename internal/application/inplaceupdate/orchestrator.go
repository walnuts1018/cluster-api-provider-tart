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

package inplaceupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/opencontainers/go-digest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	distributiondomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
	slotdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/slot"
)

const updateOperationDeadline = 2 * time.Hour

// StartInputはOSOnly Update Operationの不変入力をまとめる。
type StartInput struct {
	Machine                    *clusterv1.Machine
	TartMachine                *infrastructurev1beta1.TartMachine
	BootstrapConfig            runtime.RawExtension
	Host                       *infrastructurev1beta1.TartHost
	PlanDigest                 string
	NodeLifecyclePlanDigest    string
	TargetImageDigest          string
	TargetArtifactGeneration   uint64
	CurrentDistributionVersion string
	TargetDistributionVersion  string
	NodeRole                   distributiondomain.NodeRole
	Now                        time.Time
}

// OperationStarterはOperation作成の永続化境界である。
type OperationStarter interface {
	Start(
		context.Context,
		*infrastructurev1beta1.TartHostOperation,
	) (*infrastructurev1beta1.TartHostOperation, error)
}

// OrchestratorはUpdate Operationの生成と冪等な開始を組み立てる。
type Orchestrator struct {
	operations OperationStarter
}

// NewOrchestratorはUpdate Orchestratorを生成する。
func NewOrchestrator(operations OperationStarter) *Orchestrator {
	return &Orchestrator{operations: operations}
}

// Startは同じdesired objectsに対して同じOperation IDを使ってOperationを開始する。
func (orchestrator *Orchestrator) Start(
	ctx context.Context,
	input StartInput,
) (*infrastructurev1beta1.TartHostOperation, error) {
	operation, err := BuildOperation(input)
	if err != nil {
		return nil, err
	}
	started, err := orchestrator.operations.Start(ctx, operation)
	if err != nil {
		return nil, fmt.Errorf("start Update Operation: %w", err)
	}
	return started, nil
}

// BuildOperationは検証済み入力からOSOnly Update Operationを構築する。
func BuildOperation(input StartInput) (*infrastructurev1beta1.TartHostOperation, error) {
	if err := validateStartInput(input, true); err != nil {
		return nil, err
	}
	return buildOperation(input)
}

// BuildOperationDraftはPlan生成に必要な決定的IDとdeadlineを先に構築する。
// PlanDigestは署名済みPlanの生成後に設定する。
func BuildOperationDraft(input StartInput) (*infrastructurev1beta1.TartHostOperation, error) {
	if err := validateStartInput(input, false); err != nil {
		return nil, err
	}
	return buildOperation(input)
}

func buildOperation(input StartInput) (*infrastructurev1beta1.TartHostOperation, error) {
	active, err := slotdomain.Parse(string(input.TartMachine.Status.ActiveSlot))
	if err != nil {
		return nil, fmt.Errorf("parse active slot: %w", err)
	}
	target, err := active.Inactive()
	if err != nil {
		return nil, fmt.Errorf("select inactive slot: %w", err)
	}
	objectsDigest, err := desiredObjectsDigest(input)
	if err != nil {
		return nil, err
	}
	operationID, err := operationdomain.DeterministicID(
		string(input.Host.UID) + "/" + string(input.TartMachine.UID) + "/" + objectsDigest,
	)
	if err != nil {
		return nil, fmt.Errorf("generate deterministic Update Operation ID: %w", err)
	}
	generation := int64(input.TargetArtifactGeneration)

	return &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: input.TartMachine.Namespace,
		},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: operationID.String(),
			Type:        infrastructurev1beta1.OperationTypeUpdate,
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: input.Host.Namespace,
				Name:      input.Host.Name,
				UID:       input.Host.UID,
			},
			MachineRef: &infrastructurev1beta1.ResourceReference{
				Namespace: input.TartMachine.Namespace,
				Name:      input.TartMachine.Name,
				UID:       input.TartMachine.UID,
			},
			PlanDigest:                input.PlanDigest,
			NodeLifecyclePlanDigest:   input.NodeLifecyclePlanDigest,
			DesiredObjectsDigest:      objectsDigest,
			TargetImageDigest:         input.TargetImageDigest,
			TargetArtifactGeneration:  &generation,
			TargetSlot:                infrastructurev1beta1.OSSlot(target),
			UpdateClass:               updateClass(input),
			TargetDistributionVersion: targetDistributionVersion(input),
			Deadline:                  metav1.NewTime(input.Now.UTC().Add(updateOperationDeadline)),
		},
	}, nil
}

func validateStartInput(input StartInput, requirePlanDigest bool) error {
	switch {
	case input.Machine == nil:
		return fmt.Errorf("CAPI Machine is required")
	case input.TartMachine == nil:
		return fmt.Errorf("TartMachine is required")
	case input.Host == nil:
		return fmt.Errorf("TartHost is required")
	case input.Machine.UID == "":
		return fmt.Errorf("CAPI Machine UID is required")
	case input.TartMachine.UID == "":
		return fmt.Errorf("TartMachine UID is required")
	case input.Host.UID == "":
		return fmt.Errorf("TartHost UID is required")
	case input.TartMachine.Spec.UpdatePolicy.Mode != infrastructurev1beta1.UpdateModeInPlace:
		return fmt.Errorf("TartMachine updatePolicy must be InPlace")
	case input.TartMachine.Status.HostRef == nil:
		return fmt.Errorf("TartMachine hostRef is required")
	case !sameHostReference(input.TartMachine.Status.HostRef, input.Host):
		return fmt.Errorf("TartMachine hostRef does not identify TartHost")
	case input.Host.Status.Phase != infrastructurev1beta1.TartHostPhaseProvisioned:
		return fmt.Errorf("TartHost phase must be Provisioned, got %q", input.Host.Status.Phase)
	case input.Now.IsZero():
		return fmt.Errorf("operation start time is required")
	case input.TargetArtifactGeneration == 0 || input.TargetArtifactGeneration > math.MaxInt64:
		return fmt.Errorf("target Artifact generation must fit a positive int64")
	}
	if requirePlanDigest {
		if err := validateDigest("plan", input.PlanDigest); err != nil {
			return err
		}
		if input.NodeLifecyclePlanDigest != "" {
			if err := validateDigest("node lifecycle plan", input.NodeLifecyclePlanDigest); err != nil {
				return err
			}
		}
	}
	if err := validateDigest("target image", input.TargetImageDigest); err != nil {
		return err
	}
	return nil
}

func updateClass(input StartInput) infrastructurev1beta1.UpdateClass {
	if currentDistributionVersion(input) != targetDistributionVersion(input) {
		return infrastructurev1beta1.UpdateClassKubernetesBinary
	}
	return infrastructurev1beta1.UpdateClassOSOnly
}

func currentDistributionVersion(input StartInput) string {
	if input.CurrentDistributionVersion != "" {
		return input.CurrentDistributionVersion
	}
	return input.Machine.Spec.Version
}

func targetDistributionVersion(input StartInput) string {
	if input.TargetDistributionVersion != "" {
		return input.TargetDistributionVersion
	}
	return input.Machine.Spec.Version
}

func sameHostReference(
	reference *infrastructurev1beta1.ResourceReference,
	host *infrastructurev1beta1.TartHost,
) bool {
	return reference.Namespace == host.Namespace &&
		reference.Name == host.Name &&
		reference.UID == host.UID
}

func validateDigest(name, value string) error {
	parsed, err := digest.Parse(value)
	if err != nil || parsed.Algorithm() != digest.SHA256 || parsed.String() != value {
		return fmt.Errorf("%s digest must be a canonical SHA-256 digest", name)
	}
	return nil
}

func desiredObjectsDigest(input StartInput) (string, error) {
	bootstrapSpec, err := bootstrapSpec(input.BootstrapConfig)
	if err != nil {
		return "", err
	}
	value := struct {
		MachineUID               string                                `json:"machineUID"`
		MachineSpec              clusterv1.MachineSpec                 `json:"machineSpec"`
		TartMachineUID           string                                `json:"tartMachineUID"`
		TartMachineSpec          infrastructurev1beta1.TartMachineSpec `json:"tartMachineSpec"`
		BootstrapSpec            any                                   `json:"bootstrapSpec"`
		TargetImageDigest        string                                `json:"targetImageDigest"`
		TargetArtifactGeneration uint64                                `json:"targetArtifactGeneration"`
	}{
		MachineUID:               string(input.Machine.UID),
		MachineSpec:              input.Machine.Spec,
		TartMachineUID:           string(input.TartMachine.UID),
		TartMachineSpec:          input.TartMachine.Spec,
		BootstrapSpec:            bootstrapSpec,
		TargetImageDigest:        input.TargetImageDigest,
		TargetArtifactGeneration: input.TargetArtifactGeneration,
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal desired objects: %w", err)
	}
	canonical, err := jsoncanonicalizer.Transform(encoded)
	if err != nil {
		return "", fmt.Errorf("canonicalize desired objects: %w", err)
	}
	return digest.FromBytes(canonical).String(), nil
}

func bootstrapSpec(extension runtime.RawExtension) (any, error) {
	if len(extension.Raw) == 0 && extension.Object == nil {
		return nil, nil
	}
	raw := extension.Raw
	if len(raw) == 0 {
		var err error
		raw, err = json.Marshal(extension.Object)
		if err != nil {
			return nil, fmt.Errorf("marshal BootstrapConfig: %w", err)
		}
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("decode BootstrapConfig: %w", err)
	}
	return object["spec"], nil
}
