package client

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

func TestNewRequiresCredentialFreeHTTPSBaseURL(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trust := agentprotocol.StaticTrustStore{"test": publicKey}
	for _, baseURL := range []string{
		"http://controller.test.walnuts.dev",
		"https://token@controller.test.walnuts.dev",
		"https://controller.test.walnuts.dev?token=secret",
		"https://controller.test.walnuts.dev/api",
	} {
		t.Run(baseURL, func(t *testing.T) {
			if _, err := New(Config{BaseURL: baseURL, TrustStore: trust}); err == nil {
				t.Fatalf("New(%q) succeeded", baseURL)
			}
		})
	}
}

func TestRegisterDoesNotPutCredentialInURLOrAuthorization(t *testing.T) {
	var received agentprotocol.RegisterRequest
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.RawQuery != "" || request.Header.Get("Authorization") != "" {
			t.Errorf("registration leaked credential: URL=%q Authorization=%q", request.URL.String(), request.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		writeJSON(t, writer, agentprotocol.RegisterResponse{
			APIVersion:   agentprotocol.APIVersion,
			SessionToken: "session-secret",
			ExpiresAt:    time.Now().Add(10 * time.Minute),
			PlanDigest:   "sha256:" + strings.Repeat("a", 64),
		})
	}))
	defer server.Close()
	client := newTestClient(t, server, nil)
	request := agentprotocol.RegisterRequest{
		APIVersion:      agentprotocol.APIVersion,
		OperationUID:    "operation-uid",
		HostUID:         "host-uid",
		AgentInstanceID: "agent-instance",
		Inventory:       agentprotocol.Inventory{Disks: []agentprotocol.DiskInventory{}},
	}
	response, err := client.Register(context.Background(), request)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if response.SessionToken != "session-secret" || received.AgentInstanceID != request.AgentInstanceID {
		t.Fatalf("Register() response = %#v, request = %#v", response, received)
	}
}

func TestFetchPlanVerifiesDigestSignatureAndAuthorization(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	plan := testPlan()
	validated, err := agentprotocol.ValidatePlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := agentprotocol.Sign(validated, "test-key", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	planDigest, err := validated.Digest()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer session-secret" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		writeJSON(t, writer, agentprotocol.SignedPlan{Plan: plan, Signature: signature})
	}))
	defer server.Close()
	client := newTestClient(t, server, agentprotocol.StaticTrustStore{"test-key": publicKey})
	got, err := client.FetchPlan(context.Background(), plan.OperationUID, "session-secret", planDigest.String())
	if err != nil {
		t.Fatalf("FetchPlan() error = %v", err)
	}
	if got.Value().Artifact.Ref != plan.Artifact.Ref {
		t.Fatalf("FetchPlan() = %#v", got.Value())
	}
	if _, err := client.FetchPlan(context.Background(), plan.OperationUID, "session-secret", digest.FromString("other").String()); err == nil {
		t.Fatal("FetchPlan() accepted a mismatched digest")
	}
	untrustedClient := newTestClient(t, server, agentprotocol.StaticTrustStore{"other-key": publicKey})
	if _, err := untrustedClient.FetchPlan(context.Background(), plan.OperationUID, "session-secret", planDigest.String()); err == nil {
		t.Fatal("FetchPlan() accepted an untrusted signature key")
	}
}

func TestFetchBootstrapAndReportBootUseBoundSession(t *testing.T) {
	payload := []byte("#cloud-config\n")
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer session-secret" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case "/v1/operations/operation-uid/bootstrap":
			writeJSON(t, writer, agentprotocol.BootstrapBundle{
				APIVersion:    agentprotocol.APIVersion,
				Format:        agentprotocol.BootstrapFormatCloud,
				Payload:       payload,
				PayloadDigest: digest.FromBytes(payload).String(),
				MachineUID:    "machine-uid",
				OperationUID:  "operation-uid",
			})
		case "/v1/operations/operation-uid/boot-report":
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected path %q", request.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server, nil)
	bundle, err := client.FetchBootstrap(context.Background(), "operation-uid", "session-secret")
	if err != nil {
		t.Fatalf("FetchBootstrap() error = %v", err)
	}
	if string(bundle.Payload) != string(payload) {
		t.Fatalf("FetchBootstrap() payload = %q", bundle.Payload)
	}
	err = client.ReportBoot(context.Background(), "session-secret", agentprotocol.BootReportRequest{
		APIVersion:         agentprotocol.APIVersion,
		OperationUID:       "operation-uid",
		PlanDigest:         "sha256:" + strings.Repeat("a", 64),
		BootID:             "boot-id",
		ActiveSlot:         "A",
		ArtifactGeneration: 1,
	})
	if err != nil {
		t.Fatalf("ReportBoot() error = %v", err)
	}
}

func TestClientRetriesTemporaryStatusAtMostThreeTimes(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		current := attempts.Add(1)
		if current < 3 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeJSON(t, writer, agentprotocol.RegisterResponse{
			APIVersion:   agentprotocol.APIVersion,
			SessionToken: "session-secret",
			ExpiresAt:    time.Now().Add(10 * time.Minute),
			PlanDigest:   "sha256:" + strings.Repeat("a", 64),
		})
	}))
	defer server.Close()
	client := newTestClient(t, server, nil)
	if _, err := client.Register(context.Background(), agentprotocol.RegisterRequest{}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestClientRejectsRedirectWithoutForwardingToken(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Store(true)
	}))
	defer target.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Redirect(writer, &http.Request{}, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := newTestClient(t, server, nil)
	_, err := client.ReportProgress(context.Background(), "session-secret", agentprotocol.ProgressRequest{
		OperationUID: "operation-uid",
	})
	if err == nil {
		t.Fatal("ReportProgress() followed redirect")
	}
	if redirected.Load() {
		t.Fatal("redirect target received the session token")
	}
}

func newTestClient(
	t *testing.T,
	server *httptest.Server,
	trustStore agentprotocol.TrustStore,
) *Client {
	t.Helper()
	if trustStore == nil {
		publicKey, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		trustStore = agentprotocol.StaticTrustStore{"unused": publicKey}
	}
	client, err := New(Config{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		TrustStore: trustStore,
		RetryDelay: func(uint) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func testPlan() agentprotocol.Plan {
	return agentprotocol.Plan{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  "operation-uid",
		HostUID:       "host-uid",
		OperationType: agentprotocol.OperationTypeProvision,
		Deadline:      time.Now().Add(time.Hour).UTC(),
		RootDevice: agentprotocol.RootDevice{
			DeviceName:   "/dev/disk/by-id/test",
			SerialNumber: "serial",
			MinSizeBytes: 1,
		},
		Artifact: agentprotocol.Artifact{
			Ref:            "oci://registry.test.walnuts.dev/os@sha256:" + strings.Repeat("b", 64),
			ManifestDigest: "sha256:" + strings.Repeat("c", 64),
			Generation:     1,
		},
		AllowedTargetRoles: []agentprotocol.DiskRole{agentprotocol.DiskRoleOSA},
		Steps:              []agentprotocol.PlanStep{{Name: "WriteImage"}},
	}
}

func writeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Error(err)
	}
}
