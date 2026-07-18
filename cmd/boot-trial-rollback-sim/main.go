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

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/update_machine"
	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
	bootreportservice "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/repository/k8s/bootreport"
)

const (
	defaultOutputDir            = "dist/os-artifact/boot-trial-rollback"
	testBootstrapPayloadDigest  = "sha256:" + "1111111111111111111111111111111111111111111111111111111111111111"
	testOperationUID            = "operation-uid"
	testOperationNamespace      = "default"
	testOperationName           = "operation"
	testRollbackPhaseTransition = "BootTrial->RollingBack"
	testRollbackFinalTransition = "RollingBack->Failed"
)

type config struct {
	outputDir string
}

type staticPlanProvider struct {
	plan agentprotocol.SignedPlan
}

func (provider staticPlanProvider) GetPlan(context.Context, client.ObjectKey) (agentprotocol.SignedPlan, error) {
	return provider.plan, nil
}

type attemptEvidence struct {
	Name           string                                       `json:"name"`
	ReportedSlot   string                                       `json:"reportedSlot"`
	Phase          infrastructurev1beta1.TartHostOperationPhase `json:"phase"`
	Attempt        int32                                        `json:"attempt"`
	DegradedReason string                                       `json:"degradedReason,omitempty"`
}

type evidence struct {
	GeneratedAtUTC string            `json:"generatedAtUTC"`
	PlanDigest     string            `json:"planDigest"`
	ExpectedSlot   string            `json:"expectedSlot"`
	RollbackSlot   string            `json:"rollbackSlot"`
	Transitions    map[string]string `json:"transitions"`
	Attempts       []attemptEvidence `json:"attempts"`
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "boot trial rollback simulator failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	result, err := simulate(ctx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := writeEvidence(cfg.outputDir, result); err != nil {
		return err
	}
	if err := writeSummary(cfg.outputDir, result); err != nil {
		return err
	}
	return nil
}

func parseConfig(args []string) (config, error) {
	cfg := config{outputDir: defaultOutputDir}
	flags := flag.NewFlagSet("boot-trial-rollback-sim", flag.ContinueOnError)
	flags.StringVar(&cfg.outputDir, "output-dir", cfg.outputDir, "Directory that receives rollback simulator evidence.")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	if strings.TrimSpace(cfg.outputDir) == "" {
		return config{}, errors.New("--output-dir must not be empty")
	}
	return cfg, nil
}

func simulate(ctx context.Context) (evidence, error) {
	service, k8sClient, key, planDigest, err := newSimulationService()
	if err != nil {
		return evidence{}, err
	}
	steps := []struct {
		name   string
		report agentprotocol.BootReportRequest
	}{
		{
			name: "wrong-slot-attempt-1",
			report: agentprotocol.BootReportRequest{
				APIVersion:             agentprotocol.APIVersion,
				OperationUID:           testOperationUID,
				PlanDigest:             planDigest,
				BootID:                 "wrong-slot-boot-1",
				MachineID:              "machine-id",
				ActiveSlot:             "A",
				ArtifactGeneration:     1,
				StateMounted:           true,
				DataMounted:            true,
				BootstrapApplied:       true,
				BootstrapPayloadDigest: testBootstrapPayloadDigest,
			},
		},
		{
			name: "wrong-slot-attempt-2",
			report: agentprotocol.BootReportRequest{
				APIVersion:             agentprotocol.APIVersion,
				OperationUID:           testOperationUID,
				PlanDigest:             planDigest,
				BootID:                 "wrong-slot-boot-2",
				MachineID:              "machine-id",
				ActiveSlot:             "A",
				ArtifactGeneration:     1,
				StateMounted:           true,
				DataMounted:            true,
				BootstrapApplied:       true,
				BootstrapPayloadDigest: testBootstrapPayloadDigest,
			},
		},
		{
			name: "wrong-slot-attempt-3",
			report: agentprotocol.BootReportRequest{
				APIVersion:             agentprotocol.APIVersion,
				OperationUID:           testOperationUID,
				PlanDigest:             planDigest,
				BootID:                 "wrong-slot-boot-3",
				MachineID:              "machine-id",
				ActiveSlot:             "A",
				ArtifactGeneration:     1,
				StateMounted:           true,
				DataMounted:            true,
				BootstrapApplied:       true,
				BootstrapPayloadDigest: testBootstrapPayloadDigest,
			},
		},
		{
			name: "target-slot-report-during-rollback",
			report: agentprotocol.BootReportRequest{
				APIVersion:             agentprotocol.APIVersion,
				OperationUID:           testOperationUID,
				PlanDigest:             planDigest,
				BootID:                 "target-slot-boot-during-rollback",
				MachineID:              "machine-id",
				ActiveSlot:             "B",
				ArtifactGeneration:     2,
				StateMounted:           true,
				DataMounted:            true,
				BootstrapApplied:       true,
				BootstrapPayloadDigest: testBootstrapPayloadDigest,
			},
		},
		{
			name: "healthy-rollback-boot",
			report: agentprotocol.BootReportRequest{
				APIVersion:             agentprotocol.APIVersion,
				OperationUID:           testOperationUID,
				PlanDigest:             planDigest,
				BootID:                 "healthy-rollback-boot",
				MachineID:              "machine-id",
				ActiveSlot:             "A",
				ArtifactGeneration:     1,
				StateMounted:           true,
				DataMounted:            true,
				BootstrapApplied:       true,
				BootstrapPayloadDigest: testBootstrapPayloadDigest,
			},
		},
	}

	attempts := make([]attemptEvidence, 0, len(steps))
	for _, step := range steps {
		if err := service.ReportBoot(ctx, key, step.report, metav1.Now()); err != nil {
			return evidence{}, fmt.Errorf("report %s: %w", step.name, err)
		}
		current, err := loadOperation(ctx, k8sClient, key)
		if err != nil {
			return evidence{}, err
		}
		attempts = append(attempts, attemptEvidence{
			Name:           step.name,
			ReportedSlot:   step.report.ActiveSlot,
			Phase:          current.Status.Phase,
			Attempt:        current.Status.Attempt,
			DegradedReason: degradedReason(current.Status.Conditions),
		})
	}

	if err := verifySimulation(attempts); err != nil {
		return evidence{}, err
	}
	return evidence{
		GeneratedAtUTC: time.Now().UTC().Format(time.RFC3339),
		PlanDigest:     planDigest,
		ExpectedSlot:   "B",
		RollbackSlot:   "A",
		Transitions: map[string]string{
			"rollbackStart": testRollbackPhaseTransition,
			"rollbackFinal": testRollbackFinalTransition,
		},
		Attempts: attempts,
	}, nil
}

func newSimulationService() (*bootreportservice.Service, client.Client, client.ObjectKey, string, error) {
	plan := agentprotocol.Plan{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  testOperationUID,
		HostUID:       "host-uid",
		OperationType: agentprotocol.OperationTypeUpdate,
		ActiveSlot:    "A",
		Deadline:      time.Date(2026, 7, 17, 13, 0, 0, 0, time.UTC),
		RootDevice: agentprotocol.RootDevice{
			DeviceName:   "/dev/disk/by-id/wwn-disk",
			SerialNumber: "disk-serial",
			MinSizeBytes: 1,
		},
		Artifact: &agentprotocol.Artifact{
			Ref:            "oci://registry.test.walnuts.dev/os@sha256:" + strings.Repeat("b", 64),
			ManifestDigest: "sha256:" + strings.Repeat("c", 64),
			Generation:     2,
		},
		AllowedTargetRoles: []agentprotocol.DiskRole{
			agentprotocol.DiskRoleOSB,
			agentprotocol.DiskRoleVerityB,
		},
		Steps: []agentprotocol.PlanStep{{Name: "BootTrial"}},
	}
	validated, err := agentprotocol.ValidatePlan(plan)
	if err != nil {
		return nil, nil, client.ObjectKey{}, "", fmt.Errorf("validate plan: %w", err)
	}
	digest, err := validated.Digest()
	if err != nil {
		return nil, nil, client.ObjectKey{}, "", fmt.Errorf("digest plan: %w", err)
	}
	key := client.ObjectKey{Namespace: testOperationNamespace, Name: testOperationName}
	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: testOperationUID,
			Type:        infrastructurev1beta1.OperationTypeUpdate,
			PlanDigest:  digest.String(),
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: testOperationNamespace,
				Name:      "host",
				UID:       types.UID("host-uid"),
			},
		},
		Status: infrastructurev1beta1.TartHostOperationStatus{
			Phase: infrastructurev1beta1.TartHostOperationPhaseBootTrial,
		},
	}
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		return nil, nil, client.ObjectKey{}, "", fmt.Errorf("add scheme: %w", err)
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(operation).
		Build()
	return bootreportservice.NewService(
		k8sClient,
		staticPlanProvider{plan: agentprotocol.SignedPlan{Plan: plan}},
	), k8sClient, key, digest.String(), nil
}

func loadOperation(
	ctx context.Context,
	k8sClient client.Client,
	key client.ObjectKey,
) (*infrastructurev1beta1.TartHostOperation, error) {
	operation := &infrastructurev1beta1.TartHostOperation{}
	if err := k8sClient.Get(ctx, key, operation); err != nil {
		return nil, fmt.Errorf("load operation: %w", err)
	}
	return operation, nil
}

func degradedReason(conditions []metav1.Condition) string {
	condition := apimeta.FindStatusCondition(conditions, appupdate.ConditionDegraded)
	if condition == nil {
		return ""
	}
	return condition.Reason
}

func verifySimulation(attempts []attemptEvidence) error {
	if len(attempts) != 5 {
		return fmt.Errorf("simulation recorded %d attempts, want 5", len(attempts))
	}
	if attempts[0].Phase != infrastructurev1beta1.TartHostOperationPhaseBootTrial || attempts[0].Attempt != 1 {
		return fmt.Errorf("attempt 1 = %#v, want BootTrial attempt 1", attempts[0])
	}
	if attempts[1].Phase != infrastructurev1beta1.TartHostOperationPhaseBootTrial || attempts[1].Attempt != 2 {
		return fmt.Errorf("attempt 2 = %#v, want BootTrial attempt 2", attempts[1])
	}
	if attempts[2].Phase != infrastructurev1beta1.TartHostOperationPhaseRollingBack || attempts[2].Attempt != 3 || attempts[2].DegradedReason != "BootFailed" {
		return fmt.Errorf("attempt 3 = %#v, want RollingBack attempt 3 with BootFailed", attempts[2])
	}
	if attempts[3].Phase != infrastructurev1beta1.TartHostOperationPhaseRollingBack || attempts[3].Attempt != 3 {
		return fmt.Errorf("attempt 4 = %#v, want RollingBack attempt 3", attempts[3])
	}
	if attempts[4].Phase != infrastructurev1beta1.TartHostOperationPhaseFailed || attempts[4].Attempt != 3 || attempts[4].DegradedReason != "BootFailed" {
		return fmt.Errorf("attempt 5 = %#v, want Failed attempt 3 with retained BootFailed", attempts[4])
	}
	return nil
}

func writeEvidence(outputDir string, result evidence) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encode evidence: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "evidence.json"), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write evidence.json: %w", err)
	}
	return nil
}

func writeSummary(outputDir string, result evidence) error {
	lines := make([]string, 0, 4+len(result.Attempts))
	lines = append(lines,
		"Task 01 boot trial rollback simulator",
		"planDigest="+result.PlanDigest,
		"expectedSlot="+result.ExpectedSlot,
		"rollbackSlot="+result.RollbackSlot,
	)
	for _, attempt := range result.Attempts {
		lines = append(lines, fmt.Sprintf(
			"%s reportedSlot=%s phase=%s attempt=%d degraded=%s",
			attempt.Name,
			attempt.ReportedSlot,
			attempt.Phase,
			attempt.Attempt,
			attempt.DegradedReason,
		))
	}
	if err := os.WriteFile(filepath.Join(outputDir, "summary.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return fmt.Errorf("write summary.txt: %w", err)
	}
	return nil
}
