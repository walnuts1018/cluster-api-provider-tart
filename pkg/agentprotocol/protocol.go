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
)

const (
	APIVersion               = "infrastructure.cluster.x-k8s.io/v1"
	BootstrapFormatCloud     = "cloud-config"
	MaxRequestBodyBytes      = 1 << 20
	MaxBootstrapBodyBytes    = 16 << 20
	MaxBootstrapPayloadBytes = (MaxBootstrapBodyBytes - 4096) * 3 / 4
	SignatureAlgorithm       = "Ed25519"
	MinimumTokenEntropyBit   = 256
)

var (
	uidPattern         = regexp.MustCompile(`^[a-zA-Z0-9][-a-zA-Z0-9_.:]{0,127}$`)
	artifactRefPattern = regexp.MustCompile(`^oci://[^@[:space:]]+@sha256:[0-9a-f]{64}$`)
	diskRoleValues     = map[DiskRole]struct{}{
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

type Plan struct {
	APIVersion         string           `json:"apiVersion"`
	OperationUID       string           `json:"operationUID"`
	HostUID            string           `json:"hostUID"`
	Deadline           time.Time        `json:"deadline"`
	RootDevice         RootDevice       `json:"rootDevice"`
	Artifact           Artifact         `json:"artifact"`
	AllowedTargetRoles []DiskRole       `json:"allowedTargetRoles"`
	Steps              []PlanStep       `json:"steps"`
	Bootstrap          *BootstrapTarget `json:"bootstrap,omitempty"`
}

type RootDevice struct {
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
	DevicePath   string `json:"devicePath"`
	SerialNumber string `json:"serialNumber,omitempty"`
	WWN          string `json:"wwn,omitempty"`
	SizeBytes    int64  `json:"sizeBytes"`
}

type RegisterResponse struct {
	APIVersion   string    `json:"apiVersion"`
	SessionToken string    `json:"sessionToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
	PlanDigest   string    `json:"planDigest"`
}

type ProgressRequest struct {
	APIVersion    string `json:"apiVersion"`
	OperationUID  string `json:"operationUID"`
	PlanDigest    string `json:"planDigest"`
	AgentSequence int64  `json:"agentSequence"`
	CompletedStep string `json:"completedStep"`
}

type ProgressResponse struct {
	APIVersion     string   `json:"apiVersion"`
	AgentSequence  int64    `json:"agentSequence"`
	CompletedSteps []string `json:"completedSteps"`
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
	APIVersion         string `json:"apiVersion"`
	OperationUID       string `json:"operationUID"`
	PlanDigest         string `json:"planDigest"`
	BootID             string `json:"bootID"`
	ActiveSlot         string `json:"activeSlot"`
	ArtifactGeneration uint64 `json:"artifactGeneration"`
	StateMounted       bool   `json:"stateMounted"`
	DataMounted        bool   `json:"dataMounted"`
	BootstrapApplied   bool   `json:"bootstrapApplied"`
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
	switch {
	case plan.APIVersion != APIVersion:
		return ValidatedPlan{}, fmt.Errorf("unsupported apiVersion: %q", plan.APIVersion)
	case !validUID(plan.OperationUID):
		return ValidatedPlan{}, errors.New("operationUID is invalid")
	case !validUID(plan.HostUID):
		return ValidatedPlan{}, errors.New("hostUID is invalid")
	case plan.Deadline.IsZero():
		return ValidatedPlan{}, errors.New("deadline is required")
	case plan.RootDevice.MinSizeBytes <= 0:
		return ValidatedPlan{}, errors.New("rootDevice.minSizeBytes must be greater than zero")
	case plan.RootDevice.SerialNumber == "" && plan.RootDevice.WWN == "":
		return ValidatedPlan{}, errors.New("rootDevice requires serialNumber or wwn")
	case !artifactRefPattern.MatchString(plan.Artifact.Ref):
		return ValidatedPlan{}, errors.New("artifact.ref must be a digest-pinned OCI reference")
	case !validSHA256Digest(plan.Artifact.ManifestDigest):
		return ValidatedPlan{}, errors.New("artifact.manifestDigest must be a canonical SHA-256 digest")
	case plan.Artifact.Generation == 0:
		return ValidatedPlan{}, errors.New("artifact.generation must be greater than zero")
	case len(plan.AllowedTargetRoles) == 0:
		return ValidatedPlan{}, errors.New("allowedTargetRoles must not be empty")
	case len(plan.Steps) == 0:
		return ValidatedPlan{}, errors.New("steps must not be empty")
	}

	roles := make(map[DiskRole]struct{}, len(plan.AllowedTargetRoles))
	for _, role := range plan.AllowedTargetRoles {
		if _, ok := diskRoleValues[role]; !ok {
			return ValidatedPlan{}, fmt.Errorf("unsupported target disk role: %q", role)
		}
		if _, exists := roles[role]; exists {
			return ValidatedPlan{}, fmt.Errorf("duplicate target disk role: %q", role)
		}
		roles[role] = struct{}{}
	}

	stepNames := make(map[string]struct{}, len(plan.Steps))
	for _, step := range plan.Steps {
		if !uidPattern.MatchString(step.Name) {
			return ValidatedPlan{}, fmt.Errorf("invalid step name: %q", step.Name)
		}
		if _, exists := stepNames[step.Name]; exists {
			return ValidatedPlan{}, fmt.Errorf("duplicate step name: %q", step.Name)
		}
		stepNames[step.Name] = struct{}{}
	}

	if plan.Bootstrap != nil {
		if !validUID(plan.Bootstrap.MachineUID) {
			return ValidatedPlan{}, errors.New("bootstrap.machineUID is invalid")
		}
		if plan.Bootstrap.Format != BootstrapFormatCloud {
			return ValidatedPlan{}, fmt.Errorf("unsupported bootstrap format: %q", plan.Bootstrap.Format)
		}
	}
	return ValidatedPlan{plan: plan}, nil
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
