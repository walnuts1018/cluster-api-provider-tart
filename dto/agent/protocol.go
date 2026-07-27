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

package agentprotocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/opencontainers/go-digest"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/ocireference"
)

const (
	APIVersion               = "infrastructure.cluster.x-k8s.io/v1"
	BootstrapFormatCloud     = "cloud-config"
	StepWriteImage           = "WriteImage"
	StepVerifyImage          = "VerifyImage"
	MaxRequestBodyBytes      = 1 << 20
	MaxBootstrapBodyBytes    = 16 << 20
	MaxBootstrapPayloadBytes = (MaxBootstrapBodyBytes - 4096) * 3 / 4
	SignatureAlgorithm       = "Ed25519"
	MinimumTokenEntropyBit   = 256
)

var (
	uidPattern        = regexp.MustCompile(`^[a-zA-Z0-9][-a-zA-Z0-9_.:]{0,127}$`)
	deviceByIDPattern = regexp.MustCompile(`^/dev/disk/by-id/.+$`)
	diskRoleValues    = map[DiskRole]struct{}{
		DiskRoleBoot:    {},
		DiskRoleOSA:     {},
		DiskRoleOSB:     {},
		DiskRoleVerityA: {},
		DiskRoleVerityB: {},
		DiskRoleState:   {},
		DiskRoleData:    {},
	}
	ErrUnsupportedBootstrapFormat = errors.New("unsupported bootstrap format")
	ErrBootstrapTooLarge          = errors.New("bootstrap response exceeds 16 MiB")
)

type DiskRole string

const (
	DiskRoleBoot    DiskRole = "Boot"
	DiskRoleOSA     DiskRole = "OS-A"
	DiskRoleOSB     DiskRole = "OS-B"
	DiskRoleVerityA DiskRole = "Verity-A"
	DiskRoleVerityB DiskRole = "Verity-B"
	DiskRoleState   DiskRole = "State"
	DiskRoleData    DiskRole = "Data"
)

type OperationType string

const (
	OperationTypeProvision OperationType = "Provision"
	OperationTypeUpdate    OperationType = "Update"
	OperationTypeClean     OperationType = "Clean"
	OperationTypeWipeAll   OperationType = "WipeAll"
)

type Plan struct {
	APIVersion         string           `json:"apiVersion"`
	OperationUID       string           `json:"operationUID"`
	HostUID            string           `json:"hostUID"`
	OperationType      OperationType    `json:"operationType"`
	ActiveSlot         string           `json:"activeSlot,omitempty"`
	Deadline           time.Time        `json:"deadline"`
	RootDevice         RootDevice       `json:"rootDevice"`
	Artifact           *Artifact        `json:"artifact,omitempty"`
	AllowedTargetRoles []DiskRole       `json:"allowedTargetRoles"`
	Steps              []PlanStep       `json:"steps"`
	Bootstrap          *BootstrapTarget `json:"bootstrap,omitempty"`
}

type RootDevice struct {
	DeviceName   string `json:"deviceName"`
	SerialNumber string `json:"serialNumber,omitempty"`
	WWN          string `json:"wwn,omitempty"`
	MinSizeBytes int64  `json:"minSizeBytes"`
}

type Artifact struct {
	Ref            string `json:"ref"`
	ManifestDigest string `json:"manifestDigest"`
	Generation     uint64 `json:"generation"`
}

type PlanStep struct {
	Name string `json:"name"`
}

type BootstrapTarget struct {
	MachineUID string `json:"machineUID"`
	Format     string `json:"format"`
}

type ValidatedPlan struct {
	plan Plan
}

type Signature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyID"`
	Value     string `json:"value"`
}

type SignedPlan struct {
	Plan      Plan      `json:"plan"`
	Signature Signature `json:"signature"`
}

type RegisterRequest struct {
	APIVersion      string    `json:"apiVersion"`
	OperationUID    string    `json:"operationUID"`
	HostUID         string    `json:"hostUID"`
	AgentInstanceID string    `json:"agentInstanceID"`
	Inventory       Inventory `json:"inventory"`
}

type Inventory struct {
	SystemUUID     string          `json:"systemUUID,omitempty"`
	BootMACAddress string          `json:"bootMACAddress,omitempty"`
	Disks          []DiskInventory `json:"disks"`
}

type DiskInventory struct {
	DevicePath   string   `json:"devicePath"`
	ByIDPaths    []string `json:"byIDPaths"`
	SerialNumber string   `json:"serialNumber,omitempty"`
	WWN          string   `json:"wwn,omitempty"`
	SizeBytes    int64    `json:"sizeBytes"`
	HoldsAgentOS bool     `json:"holdsAgentOS"`
}

type RegisterResponse struct {
	APIVersion    string    `json:"apiVersion"`
	SessionToken  string    `json:"sessionToken"`
	ExpiresAt     time.Time `json:"expiresAt"`
	PlanDigest    string    `json:"planDigest"`
	AgentSequence int64     `json:"agentSequence"`
}

type ProgressRequest struct {
	APIVersion    string   `json:"apiVersion"`
	OperationUID  string   `json:"operationUID"`
	PlanDigest    string   `json:"planDigest"`
	AgentSequence int64    `json:"agentSequence"`
	Step          string   `json:"step"`
	DiskRole      DiskRole `json:"diskRole,omitempty"`
	Percent       int32    `json:"percent"`
	Completed     bool     `json:"completed"`
}

type NodeLifecycleResult string

const (
	NodeLifecycleResultSucceeded NodeLifecycleResult = "Succeeded"
	NodeLifecycleResultFailed    NodeLifecycleResult = "Failed"
)

type NodeLifecycleProgressRequest struct {
	APIVersion   string              `json:"apiVersion"`
	OperationUID string              `json:"operationUID"`
	PlanDigest   string              `json:"planDigest"`
	Step         string              `json:"step"`
	Result       NodeLifecycleResult `json:"result"`
	SnapshotRef  string              `json:"snapshotRef,omitempty"`
}

type ProgressResponse struct {
	APIVersion     string          `json:"apiVersion"`
	AgentSequence  int64           `json:"agentSequence"`
	Progress       *ProgressStatus `json:"progress,omitempty"`
	CompletedSteps []string        `json:"completedSteps"`
}

type ProgressStatus struct {
	Step     string   `json:"step"`
	DiskRole DiskRole `json:"diskRole,omitempty"`
	Percent  int32    `json:"percent"`
}

type BootstrapBundle struct {
	APIVersion    string `json:"apiVersion"`
	Format        string `json:"format"`
	Payload       []byte `json:"payload"`
	PayloadDigest string `json:"payloadDigest"`
	MachineUID    string `json:"machineUID"`
	OperationUID  string `json:"operationUID"`
}

type BootReportRequest struct {
	APIVersion             string `json:"apiVersion"`
	OperationUID           string `json:"operationUID"`
	PlanDigest             string `json:"planDigest"`
	BootID                 string `json:"bootID"`
	MachineID              string `json:"machineID"`
	ActiveSlot             string `json:"activeSlot"`
	ArtifactGeneration     uint64 `json:"artifactGeneration"`
	StateMounted           bool   `json:"stateMounted"`
	DataMounted            bool   `json:"dataMounted"`
	BootstrapApplied       bool   `json:"bootstrapApplied"`
	BootstrapPayloadDigest string `json:"bootstrapPayloadDigest,omitempty"`
}

type ErrorResponse struct {
	APIVersion string `json:"apiVersion"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func ParsePlan(data []byte) (ValidatedPlan, error) {
	var plan Plan
	if err := decodeStrict(data, &plan); err != nil {
		return ValidatedPlan{}, fmt.Errorf("decode plan: %w", err)
	}
	return ValidatePlan(plan)
}

func ValidatePlan(plan Plan) (ValidatedPlan, error) {
	if err := validatePlanBasics(plan); err != nil {
		return ValidatedPlan{}, err
	}
	if err := validatePlanArtifact(plan); err != nil {
		return ValidatedPlan{}, err
	}
	if err := validatePlanRoles(plan); err != nil {
		return ValidatedPlan{}, err
	}
	if err := validatePlanSteps(plan); err != nil {
		return ValidatedPlan{}, err
	}
	if err := validatePlanBootstrap(plan); err != nil {
		return ValidatedPlan{}, err
	}
	return ValidatedPlan{plan: plan}, nil
}

func validatePlanBasics(plan Plan) error {
	switch {
	case plan.APIVersion != APIVersion:
		return fmt.Errorf("unsupported apiVersion: %q", plan.APIVersion)
	case !validUID(plan.OperationUID):
		return errors.New("operationUID is invalid")
	case !validUID(plan.HostUID):
		return errors.New("hostUID is invalid")
	case plan.OperationType != OperationTypeProvision &&
		plan.OperationType != OperationTypeUpdate &&
		plan.OperationType != OperationTypeClean &&
		plan.OperationType != OperationTypeWipeAll:
		return fmt.Errorf("unsupported operationType: %q", plan.OperationType)
	case (plan.OperationType == OperationTypeProvision ||
		plan.OperationType == OperationTypeClean ||
		plan.OperationType == OperationTypeWipeAll) && plan.ActiveSlot != "":
		return fmt.Errorf("activeSlot must be empty for %s", plan.OperationType)
	case plan.OperationType == OperationTypeUpdate && plan.ActiveSlot != "A" && plan.ActiveSlot != "B":
		return errors.New("activeSlot must be A or B for Update")
	case plan.Deadline.IsZero():
		return errors.New("deadline is required")
	case !deviceByIDPattern.MatchString(plan.RootDevice.DeviceName):
		return errors.New("rootDevice.deviceName must use a /dev/disk/by-id path")
	case plan.RootDevice.MinSizeBytes <= 0:
		return errors.New("rootDevice.minSizeBytes must be greater than zero")
	case plan.RootDevice.SerialNumber == "" && plan.RootDevice.WWN == "":
		return errors.New("rootDevice requires serialNumber or wwn")
	case len(plan.AllowedTargetRoles) == 0 && plan.OperationType != OperationTypeClean:
		return errors.New("allowedTargetRoles must not be empty")
	case len(plan.Steps) == 0:
		return errors.New("steps must not be empty")
	}
	return nil
}

func validatePlanArtifact(plan Plan) error {
	switch plan.OperationType {
	case OperationTypeProvision, OperationTypeUpdate:
		if plan.Artifact == nil {
			return errors.New("artifact is required")
		}
		switch {
		case !validOCIImageReference(plan.Artifact.Ref):
			return errors.New("artifact.ref must be a valid OCI image reference")
		case !validSHA256Digest(plan.Artifact.ManifestDigest):
			return errors.New("artifact.manifestDigest must be a canonical SHA-256 digest")
		case plan.Artifact.Generation == 0:
			return errors.New("artifact.generation must be greater than zero")
		}
	case OperationTypeClean, OperationTypeWipeAll:
		if plan.Artifact != nil {
			return fmt.Errorf("artifact must be omitted for %s", plan.OperationType)
		}
	}
	return nil
}

func validOCIImageReference(value string) bool {
	_, err := ocireference.Parse(value)
	return err == nil
}

func validatePlanRoles(plan Plan) error {
	roles := make(map[DiskRole]struct{}, len(plan.AllowedTargetRoles))
	for _, role := range plan.AllowedTargetRoles {
		if _, ok := diskRoleValues[role]; !ok {
			return fmt.Errorf("unsupported target disk role: %q", role)
		}
		if _, exists := roles[role]; exists {
			return fmt.Errorf("duplicate target disk role: %q", role)
		}
		roles[role] = struct{}{}
	}
	return nil
}

func validatePlanSteps(plan Plan) error {
	stepNames := make(map[string]struct{}, len(plan.Steps))
	for _, step := range plan.Steps {
		if !uidPattern.MatchString(step.Name) {
			return fmt.Errorf("invalid step name: %q", step.Name)
		}
		if _, exists := stepNames[step.Name]; exists {
			return fmt.Errorf("duplicate step name: %q", step.Name)
		}
		stepNames[step.Name] = struct{}{}
	}
	return nil
}

func validatePlanBootstrap(plan Plan) error {
	if plan.Bootstrap != nil {
		if plan.OperationType != OperationTypeProvision {
			return fmt.Errorf("bootstrap must be omitted for %s", plan.OperationType)
		}
		if !validUID(plan.Bootstrap.MachineUID) {
			return errors.New("bootstrap.machineUID is invalid")
		}
		if plan.Bootstrap.Format != BootstrapFormatCloud {
			return fmt.Errorf("unsupported bootstrap format: %q", plan.Bootstrap.Format)
		}
	}
	return nil
}

func ValidateBootReport(report BootReportRequest) error {
	switch {
	case report.APIVersion != APIVersion:
		return fmt.Errorf("unsupported apiVersion: %q", report.APIVersion)
	case !validUID(report.OperationUID):
		return errors.New("operationUID is invalid")
	case !validSHA256Digest(report.PlanDigest):
		return errors.New("planDigest must be a canonical SHA-256 digest")
	case !uidPattern.MatchString(report.BootID):
		return errors.New("bootID is invalid")
	case !uidPattern.MatchString(report.MachineID):
		return errors.New("machineID is invalid")
	case report.ActiveSlot != "A" && report.ActiveSlot != "B":
		return errors.New("activeSlot must be A or B")
	case report.ArtifactGeneration == 0:
		return errors.New("artifactGeneration must be greater than zero")
	case report.BootstrapApplied && !validSHA256Digest(report.BootstrapPayloadDigest):
		return errors.New("bootstrapPayloadDigest must be a canonical SHA-256 digest when bootstrapApplied is true")
	case !report.BootstrapApplied && report.BootstrapPayloadDigest != "":
		return errors.New("bootstrapPayloadDigest must be empty when bootstrapApplied is false")
	}
	return nil
}

func ValidateProgressRequest(report ProgressRequest) error {
	switch {
	case report.APIVersion != APIVersion:
		return fmt.Errorf("unsupported apiVersion: %q", report.APIVersion)
	case !validUID(report.OperationUID):
		return errors.New("operationUID is invalid")
	case !validSHA256Digest(report.PlanDigest):
		return errors.New("planDigest must be a canonical SHA-256 digest")
	case report.AgentSequence <= 0:
		return errors.New("agentSequence must be greater than zero")
	case !uidPattern.MatchString(report.Step):
		return errors.New("step is invalid")
	}
	if report.DiskRole != "" {
		if _, ok := diskRoleValues[report.DiskRole]; !ok {
			return errors.New("diskRole is invalid")
		}
	}
	switch {
	case report.Percent < 0 || report.Percent > 100 || report.Percent%10 != 0:
		return errors.New("percent must be between 0 and 100 in increments of 10")
	case report.Completed && report.Percent != 100:
		return errors.New("completed progress must be 100 percent")
	}
	return nil
}

func ValidateNodeLifecycleProgressRequest(report NodeLifecycleProgressRequest) error {
	switch {
	case report.APIVersion != APIVersion:
		return fmt.Errorf("unsupported apiVersion: %q", report.APIVersion)
	case !validUID(report.OperationUID):
		return errors.New("operationUID is invalid")
	case !validSHA256Digest(report.PlanDigest):
		return errors.New("planDigest must be a canonical SHA-256 digest")
	case !uidPattern.MatchString(report.Step):
		return errors.New("step is invalid")
	case report.Result != NodeLifecycleResultSucceeded && report.Result != NodeLifecycleResultFailed:
		return fmt.Errorf("unsupported lifecycle result: %q", report.Result)
	}
	if report.SnapshotRef != "" && !uidPattern.MatchString(report.SnapshotRef) {
		return errors.New("snapshotRef is invalid")
	}
	return nil
}

func (plan ValidatedPlan) Value() Plan {
	return plan.plan
}

func (plan ValidatedPlan) CanonicalJSON() ([]byte, error) {
	data, err := json.Marshal(plan.plan)
	if err != nil {
		return nil, fmt.Errorf("marshal plan: %w", err)
	}
	canonical, err := jsoncanonicalizer.Transform(data)
	if err != nil {
		return nil, fmt.Errorf("canonicalize plan: %w", err)
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

func ValidateBootstrapBundle(bundle BootstrapBundle) error {
	switch {
	case bundle.APIVersion != APIVersion:
		return fmt.Errorf("unsupported apiVersion: %q", bundle.APIVersion)
	case bundle.Format != BootstrapFormatCloud:
		return fmt.Errorf("%w: %q", ErrUnsupportedBootstrapFormat, bundle.Format)
	case len(bundle.Payload) == 0:
		return errors.New("payload must not be empty")
	case len(bundle.Payload) > MaxBootstrapPayloadBytes:
		return ErrBootstrapTooLarge
	case !validSHA256Digest(bundle.PayloadDigest):
		return errors.New("payloadDigest must be a canonical SHA-256 digest")
	case digest.FromBytes(bundle.Payload).String() != bundle.PayloadDigest:
		return errors.New("payloadDigest does not match payload")
	case !validUID(bundle.MachineUID):
		return errors.New("machineUID is invalid")
	case !validUID(bundle.OperationUID):
		return errors.New("operationUID is invalid")
	}
	return nil
}

func DecodeRequest(reader io.Reader, target any) error {
	limited := io.LimitReader(reader, MaxRequestBodyBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if len(data) > MaxRequestBodyBytes {
		return ErrRequestTooLarge
	}
	if err := decodeStrict(data, target); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

var ErrRequestTooLarge = errors.New("request body exceeds the 1 MiB limit")

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("body must contain exactly one JSON value")
}

func validUID(value string) bool {
	return uidPattern.MatchString(value)
}

func validSHA256Digest(value string) bool {
	parsed, err := digest.Parse(value)
	return err == nil && parsed.Algorithm() == digest.SHA256 && parsed.String() == value
}
