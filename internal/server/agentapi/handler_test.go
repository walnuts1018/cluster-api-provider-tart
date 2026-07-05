package agentapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	k8sagentprogress "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/agentprogress"
	k8sagentsession "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/agentsession"
	k8sbootreport "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/bootreport"
	agentsessiondomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/agentsession"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

var (
	testSignedPlan = agentprotocol.SignedPlan{Plan: agentprotocol.Plan{
		APIVersion:   agentprotocol.APIVersion,
		OperationUID: "operation-uid",
		HostUID:      "host-uid",
		Deadline:     time.Date(2026, 7, 5, 13, 0, 0, 0, time.UTC),
		RootDevice: agentprotocol.RootDevice{
			SerialNumber: "disk-serial",
			MinSizeBytes: 1,
		},
		Artifact: agentprotocol.Artifact{
			Ref:            "oci://registry.test.walnuts.dev/os@sha256:" + strings.Repeat("b", 64),
			ManifestDigest: "sha256:" + strings.Repeat("c", 64),
			Generation:     1,
		},
		AllowedTargetRoles: []agentprotocol.DiskRole{agentprotocol.DiskRoleOSA},
		Steps:              []agentprotocol.PlanStep{{Name: "WriteImage"}},
	}}
	testPlanDigest = mustPlanDigest(testSignedPlan.Plan)
)

type staticResolver struct {
	key       client.ObjectKey
	operation *infrastructurev1beta1.TartHostOperation
}

func (resolver staticResolver) Resolve(
	_ context.Context,
	operationUID string,
) (client.ObjectKey, *infrastructurev1beta1.TartHostOperation, error) {
	if operationUID != resolver.operation.Spec.OperationID {
		return client.ObjectKey{}, nil, errors.New("not found")
	}
	return resolver.key, resolver.operation.DeepCopy(), nil
}

type allowRegistration struct{}

func (allowRegistration) Verify(
	context.Context,
	*infrastructurev1beta1.TartHostOperation,
	string,
	agentprotocol.RegisterRequest,
) error {
	return nil
}

type staticBootstrap struct {
	bundle agentprotocol.BootstrapBundle
}

type staticPlan struct{}

func (staticPlan) GetPlan(context.Context, client.ObjectKey) (agentprotocol.SignedPlan, error) {
	return testSignedPlan, nil
}

func (provider staticBootstrap) GetBootstrapBundle(
	context.Context,
	client.ObjectKey,
) (agentprotocol.BootstrapBundle, error) {
	return provider.bundle, nil
}

type recordingBootReporter struct {
	request agentprotocol.BootReportRequest
	err     error
}

func (reporter *recordingBootReporter) ReportBoot(
	_ context.Context,
	_ client.ObjectKey,
	request agentprotocol.BootReportRequest,
	_ metav1.Time,
) error {
	reporter.request = request
	return reporter.err
}

func TestHandlerRejectsPlainHTTPWithoutRedirect(t *testing.T) {
	handler := NewHandler(Config{})
	request := httptest.NewRequest(http.MethodGet, "/v1/operations/operation-uid/plan", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUpgradeRequired {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUpgradeRequired)
	}
	if location := response.Header().Get("Location"); location != "" {
		t.Fatalf("Location = %q, want empty", location)
	}
}

func TestHandlerRejectsInvalidSessionOnEveryProtectedEndpoint(t *testing.T) {
	payload := []byte("#cloud-config\n")
	bootstrap := staticBootstrap{bundle: agentprotocol.BootstrapBundle{
		APIVersion:    agentprotocol.APIVersion,
		Format:        agentprotocol.BootstrapFormatCloud,
		Payload:       payload,
		PayloadDigest: digest.FromBytes(payload).String(),
		MachineUID:    "machine-uid",
		OperationUID:  "operation-uid",
	}}
	handler, _ := newAuthenticatedHandler(t, bootstrap)
	handler.config.BootReports = &recordingBootReporter{}
	requests := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{
			name:   "plan",
			method: http.MethodGet,
			path:   "/v1/operations/operation-uid/plan",
		},
		{
			name:   "progress",
			method: http.MethodPost,
			path:   "/v1/operations/operation-uid/progress",
			body: agentprotocol.ProgressRequest{
				APIVersion:    agentprotocol.APIVersion,
				OperationUID:  "operation-uid",
				PlanDigest:    testPlanDigest,
				AgentSequence: 1,
				CompletedStep: "WriteImage",
			},
		},
		{
			name:   "bootstrap",
			method: http.MethodGet,
			path:   "/v1/operations/operation-uid/bootstrap",
		},
		{
			name:   "boot report",
			method: http.MethodPost,
			path:   "/v1/operations/operation-uid/boot-report",
			body: agentprotocol.BootReportRequest{
				APIVersion:         agentprotocol.APIVersion,
				OperationUID:       "operation-uid",
				PlanDigest:         testPlanDigest,
				BootID:             "boot-id",
				ActiveSlot:         "A",
				ArtifactGeneration: 1,
			},
		},
	}
	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			response := performJSONRequest(
				t,
				handler,
				request.method,
				request.path,
				"token-for-another-host-or-operation",
				request.body,
			)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusUnauthorized, response.Body.String())
			}
		})
	}
}

func TestHandlerRejectsExpiredSession(t *testing.T) {
	handler, sessionToken := newAuthenticatedHandler(t, nil)
	handler.config.Now = func() time.Time {
		return time.Date(2026, 7, 5, 12, 11, 0, 0, time.UTC)
	}

	response := performJSONRequest(
		t,
		handler,
		http.MethodGet,
		"/v1/operations/operation-uid/plan",
		sessionToken,
		nil,
	)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
}

func TestHandlerErrorResponseDoesNotReflectCredentialOrRequestValue(t *testing.T) {
	handler, _ := newAuthenticatedHandler(t, nil)
	credential := "credential-that-must-not-be-reflected"
	secretValue := "request-value-that-must-not-be-reflected"
	body := agentprotocol.ProgressRequest{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  "operation-uid",
		PlanDigest:    testPlanDigest,
		AgentSequence: 1,
		CompletedStep: secretValue,
	}

	response := performJSONRequest(
		t,
		handler,
		http.MethodPost,
		"/v1/operations/operation-uid/progress",
		credential,
		body,
	)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	for _, sensitive := range []string{credential, secretValue} {
		if strings.Contains(response.Body.String(), sensitive) {
			t.Fatalf("error response contains sensitive value %q", sensitive)
		}
	}
}

func TestHandlerProgressSequence(t *testing.T) {
	handler, sessionToken := newAuthenticatedHandler(t, nil)
	sequences := []int64{1, 2, 2, 1, 4, 3}
	wantStatuses := []int{
		http.StatusOK,
		http.StatusOK,
		http.StatusOK,
		http.StatusOK,
		http.StatusConflict,
		http.StatusOK,
	}
	for index, sequence := range sequences {
		body := agentprotocol.ProgressRequest{
			APIVersion:    agentprotocol.APIVersion,
			OperationUID:  "operation-uid",
			PlanDigest:    testPlanDigest,
			AgentSequence: sequence,
			CompletedStep: "WriteImage",
		}
		response := performJSONRequest(
			t,
			handler,
			http.MethodPost,
			"/v1/operations/operation-uid/progress",
			sessionToken,
			body,
		)
		if response.Code != wantStatuses[index] {
			t.Fatalf("sequence %d status = %d, want %d; body=%s", sequence, response.Code, wantStatuses[index], response.Body.String())
		}
	}
}

func TestHandlerRejectsProgressStepOutsidePlan(t *testing.T) {
	handler, sessionToken := newAuthenticatedHandler(t, nil)
	body := agentprotocol.ProgressRequest{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  "operation-uid",
		PlanDigest:    testPlanDigest,
		AgentSequence: 1,
		CompletedStep: "EraseState",
	}

	response := performJSONRequest(
		t,
		handler,
		http.MethodPost,
		"/v1/operations/operation-uid/progress",
		sessionToken,
		body,
	)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
	}
}

func TestHandlerAcceptsValidBootReport(t *testing.T) {
	reporter := &recordingBootReporter{}
	handler, sessionToken := newAuthenticatedHandler(t, nil)
	handler.config.BootReports = reporter
	body := agentprotocol.BootReportRequest{
		APIVersion:         agentprotocol.APIVersion,
		OperationUID:       "operation-uid",
		PlanDigest:         testPlanDigest,
		BootID:             "boot-id",
		ActiveSlot:         "A",
		ArtifactGeneration: 1,
		StateMounted:       true,
		DataMounted:        true,
		BootstrapApplied:   true,
	}

	response := performJSONRequest(
		t,
		handler,
		http.MethodPost,
		"/v1/operations/operation-uid/boot-report",
		sessionToken,
		body,
	)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if reporter.request.BootID != body.BootID {
		t.Fatalf("reported boot ID = %q, want %q", reporter.request.BootID, body.BootID)
	}
}

func TestHandlerRejectsInvalidAndConflictingBootReports(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*agentprotocol.BootReportRequest)
		reportErr  error
		wantStatus int
	}{
		{
			name: "invalid active slot",
			mutate: func(report *agentprotocol.BootReportRequest) {
				report.ActiveSlot = "C"
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "operation phase conflict",
			mutate:     func(*agentprotocol.BootReportRequest) {},
			reportErr:  k8sbootreport.ErrReportConflict,
			wantStatus: http.StatusConflict,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reporter := &recordingBootReporter{err: test.reportErr}
			handler, sessionToken := newAuthenticatedHandler(t, nil)
			handler.config.BootReports = reporter
			body := agentprotocol.BootReportRequest{
				APIVersion:         agentprotocol.APIVersion,
				OperationUID:       "operation-uid",
				PlanDigest:         testPlanDigest,
				BootID:             "boot-id",
				ActiveSlot:         "A",
				ArtifactGeneration: 1,
			}
			test.mutate(&body)

			response := performJSONRequest(
				t,
				handler,
				http.MethodPost,
				"/v1/operations/operation-uid/boot-report",
				sessionToken,
				body,
			)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

func TestHandlerBootstrapIsSingleShot(t *testing.T) {
	payload := []byte("#cloud-config\npassword: highly-secret\n")
	bundle := agentprotocol.BootstrapBundle{
		APIVersion:    agentprotocol.APIVersion,
		Format:        agentprotocol.BootstrapFormatCloud,
		Payload:       payload,
		PayloadDigest: digest.FromBytes(payload).String(),
		MachineUID:    "machine-uid",
		OperationUID:  "operation-uid",
	}
	handler, sessionToken := newAuthenticatedHandler(t, staticBootstrap{bundle: bundle})

	first := performJSONRequest(
		t,
		handler,
		http.MethodGet,
		"/v1/operations/operation-uid/bootstrap",
		sessionToken,
		nil,
	)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d; body=%s", first.Code, http.StatusOK, first.Body.String())
	}
	second := performJSONRequest(
		t,
		handler,
		http.MethodGet,
		"/v1/operations/operation-uid/bootstrap",
		sessionToken,
		nil,
	)
	if second.Code != http.StatusUnauthorized {
		t.Fatalf("second status = %d, want %d; body=%s", second.Code, http.StatusUnauthorized, second.Body.String())
	}
	if strings.Contains(second.Body.String(), "highly-secret") {
		t.Fatal("rejected response contains Bootstrap payload")
	}
}

func TestHandlerRejectsRequestLargerThanOneMiB(t *testing.T) {
	handler, sessionToken := newAuthenticatedHandler(t, nil)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/operations/operation-uid/progress",
		bytes.NewReader(make([]byte, agentprotocol.MaxRequestBodyBytes+1)),
	)
	request.TLS = &tls.ConnectionState{}
	request.Header.Set("Authorization", "Bearer "+sessionToken)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestHandlerRejectsUnsupportedFormatAndOversizedBootstrap(t *testing.T) {
	tests := []struct {
		name       string
		bundle     agentprotocol.BootstrapBundle
		wantStatus int
	}{
		{
			name: "unsupported format",
			bundle: agentprotocol.BootstrapBundle{
				APIVersion:    agentprotocol.APIVersion,
				Format:        "ignition",
				Payload:       []byte("payload"),
				PayloadDigest: digest.FromBytes([]byte("payload")).String(),
				MachineUID:    "machine-uid",
				OperationUID:  "operation-uid",
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "oversized payload",
			bundle: func() agentprotocol.BootstrapBundle {
				payload := make([]byte, agentprotocol.MaxBootstrapPayloadBytes+1)
				return agentprotocol.BootstrapBundle{
					APIVersion:    agentprotocol.APIVersion,
					Format:        agentprotocol.BootstrapFormatCloud,
					Payload:       payload,
					PayloadDigest: digest.FromBytes(payload).String(),
					MachineUID:    "machine-uid",
					OperationUID:  "operation-uid",
				}
			}(),
			wantStatus: http.StatusRequestEntityTooLarge,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, token := newAuthenticatedHandler(t, staticBootstrap{bundle: test.bundle})
			response := performJSONRequest(
				t,
				handler,
				http.MethodGet,
				"/v1/operations/operation-uid/bootstrap",
				token,
				nil,
			)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

func newAuthenticatedHandler(t *testing.T, bootstrap BootstrapProvider) (*Handler, string) {
	t.Helper()
	key := client.ObjectKey{Namespace: "default", Name: "operation"}
	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: "operation-uid",
			PlanDigest:  testPlanDigest,
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "host",
				UID:       types.UID("host-uid"),
			},
		},
	}
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(operation).
		Build()
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	sessions := k8sagentsession.NewService(k8sClient, agentsessiondomain.DefaultTTL)
	token, _, err := sessions.Issue(context.Background(), key, "host-uid", "operation-uid", now)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	return NewHandler(Config{
		Operations:           staticResolver{key: key, operation: operation},
		RegistrationVerifier: allowRegistration{},
		Sessions:             sessions,
		Progress:             k8sagentprogress.NewService(k8sClient),
		Plans:                staticPlan{},
		Bootstrap:            bootstrap,
		Now:                  func() time.Time { return now },
	}), token.BearerValue()
}

func mustPlanDigest(plan agentprotocol.Plan) string {
	validated, err := agentprotocol.ValidatePlan(plan)
	if err != nil {
		panic(err)
	}
	digest, err := validated.Digest()
	if err != nil {
		panic(err)
	}
	return digest.String()
}

func performJSONRequest(
	t *testing.T,
	handler http.Handler,
	method, path, token string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.TLS = &tls.ConnectionState{}
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
