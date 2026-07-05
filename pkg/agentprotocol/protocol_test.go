package agentprotocol

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
)

func validPlan() Plan {
	return Plan{
		APIVersion:    APIVersion,
		OperationUID:  "operation-uid",
		HostUID:       "host-uid",
		OperationType: OperationTypeProvision,
		Deadline:      time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC),
		RootDevice: RootDevice{
			DeviceName:   "/dev/disk/by-id/wwn-disk",
			SerialNumber: "disk-serial",
			MinSizeBytes: 64 << 30,
		},
		Artifact: Artifact{
			Ref:            "oci://registry.test/os@sha256:" + strings.Repeat("a", 64),
			ManifestDigest: "sha256:" + strings.Repeat("b", 64),
			Generation:     1,
		},
		AllowedTargetRoles: []DiskRole{DiskRoleBoot, DiskRoleOSA, DiskRoleVerityA, DiskRoleState, DiskRoleData},
		Steps:              []PlanStep{{Name: "WriteImage"}, {Name: "VerifyImage"}},
		Bootstrap: &BootstrapTarget{
			MachineUID: "machine-uid",
			Format:     BootstrapFormatCloud,
		},
	}
}

func TestPlanCanonicalDigestAndSignature(t *testing.T) {
	validated, err := ValidatePlan(validPlan())
	if err != nil {
		t.Fatalf("ValidatePlan() error = %v", err)
	}
	canonical, err := validated.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error = %v", err)
	}
	parsed, err := ParsePlan([]byte(" \n" + string(canonical) + "\n"))
	if err != nil {
		t.Fatalf("ParsePlan() error = %v", err)
	}
	gotDigest, err := parsed.Digest()
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	if want := digest.FromBytes(canonical); gotDigest != want {
		t.Fatalf("Digest() = %q, want %q", gotDigest, want)
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	signature, err := Sign(parsed, "test-key", privateKey)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if err := VerifySignature(parsed, signature, StaticTrustStore{"test-key": publicKey}); err != nil {
		t.Fatalf("VerifySignature() error = %v", err)
	}

	changed := validPlan()
	changed.Artifact.Generation = 2
	changedPlan, err := ValidatePlan(changed)
	if err != nil {
		t.Fatalf("ValidatePlan(changed) error = %v", err)
	}
	if err := VerifySignature(changedPlan, signature, StaticTrustStore{"test-key": publicKey}); err == nil {
		t.Fatal("VerifySignature() accepted a modified plan")
	}
}

func TestValidatePlanRejectsUnsafePlans(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Plan)
	}{
		{name: "missing operation type", mutate: func(plan *Plan) { plan.OperationType = "" }},
		{name: "unstable device name", mutate: func(plan *Plan) { plan.RootDevice.DeviceName = "/dev/sda" }},
		{name: "missing disk identity", mutate: func(plan *Plan) { plan.RootDevice.SerialNumber = "" }},
		{name: "unknown role", mutate: func(plan *Plan) { plan.AllowedTargetRoles = []DiskRole{"Unknown"} }},
		{name: "duplicate role", mutate: func(plan *Plan) { plan.AllowedTargetRoles = []DiskRole{DiskRoleOSA, DiskRoleOSA} }},
		{name: "duplicate step", mutate: func(plan *Plan) { plan.Steps = []PlanStep{{Name: "Write"}, {Name: "Write"}} }},
		{name: "unsupported bootstrap", mutate: func(plan *Plan) { plan.Bootstrap.Format = "ignition" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := validPlan()
			test.mutate(&plan)
			if _, err := ValidatePlan(plan); err == nil {
				t.Fatal("ValidatePlan() accepted invalid plan")
			}
		})
	}
}

func TestValidateBootstrapBundle(t *testing.T) {
	payload := []byte("#cloud-config\n")
	bundle := BootstrapBundle{
		APIVersion:    APIVersion,
		Format:        BootstrapFormatCloud,
		Payload:       payload,
		PayloadDigest: digest.FromBytes(payload).String(),
		MachineUID:    "machine-uid",
		OperationUID:  "operation-uid",
	}
	if err := ValidateBootstrapBundle(bundle); err != nil {
		t.Fatalf("ValidateBootstrapBundle() error = %v", err)
	}
	bundle.Payload = []byte("changed")
	if err := ValidateBootstrapBundle(bundle); err == nil {
		t.Fatal("ValidateBootstrapBundle() accepted a digest mismatch")
	}
}

func TestValidateProgressRequest(t *testing.T) {
	valid := ProgressRequest{
		APIVersion:    APIVersion,
		OperationUID:  "operation-uid",
		PlanDigest:    "sha256:" + strings.Repeat("a", 64),
		AgentSequence: 1,
		Step:          StepWriteImage,
		DiskRole:      DiskRoleOSA,
		Percent:       10,
	}
	if err := ValidateProgressRequest(valid); err != nil {
		t.Fatalf("ValidateProgressRequest() error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*ProgressRequest)
	}{
		{name: "zero sequence", mutate: func(request *ProgressRequest) { request.AgentSequence = 0 }},
		{name: "unknown disk role", mutate: func(request *ProgressRequest) { request.DiskRole = "Unknown" }},
		{name: "non ten percent increment", mutate: func(request *ProgressRequest) { request.Percent = 15 }},
		{name: "completed below 100 percent", mutate: func(request *ProgressRequest) {
			request.Percent = 90
			request.Completed = true
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.mutate(&request)
			if err := ValidateProgressRequest(request); err == nil {
				t.Fatal("ValidateProgressRequest() accepted invalid progress")
			}
		})
	}
}

func TestDecodeRequestEnforcesLimitAndStrictJSON(t *testing.T) {
	var request ProgressRequest
	if err := DecodeRequest(bytes.NewBufferString(`{"unknown":true}`), &request); err == nil {
		t.Fatal("DecodeRequest() accepted an unknown field")
	}
	tooLarge := bytes.NewReader(make([]byte, MaxRequestBodyBytes+1))
	if err := DecodeRequest(tooLarge, &request); !errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("DecodeRequest() error = %v, want %v", err, ErrRequestTooLarge)
	}
}
