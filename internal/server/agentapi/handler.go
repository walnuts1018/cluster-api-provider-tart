package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/time/rate"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	k8sagentprogress "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/agentprogress"
	k8sbootreport "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/bootreport"
	agentprogressdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/agentprogress"
	agentsessiondomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/agentsession"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

type OperationResolver interface {
	Resolve(context.Context, string) (client.ObjectKey, *infrastructurev1beta1.TartHostOperation, error)
}

type RegistrationVerifier interface {
	Verify(context.Context, *infrastructurev1beta1.TartHostOperation, string, agentprotocol.RegisterRequest) error
}

type SessionService interface {
	Issue(context.Context, client.ObjectKey, string, string, time.Time) (agentsessiondomain.Token, time.Time, error)
	Authenticate(context.Context, client.ObjectKey, string, string, string, time.Time) error
	ClaimBootstrap(context.Context, client.ObjectKey, string, string, string, time.Time) error
}

type ProgressService interface {
	Report(context.Context, client.ObjectKey, string, string, int64, string) (k8sagentprogress.Result, error)
}

type PlanProvider interface {
	GetPlan(context.Context, client.ObjectKey) (agentprotocol.SignedPlan, error)
}

type BootstrapProvider interface {
	GetBootstrapBundle(context.Context, client.ObjectKey) (agentprotocol.BootstrapBundle, error)
}

type BootReporter interface {
	ReportBoot(context.Context, client.ObjectKey, agentprotocol.BootReportRequest, metav1.Time) error
}

type Config struct {
	Operations           OperationResolver
	RegistrationVerifier RegistrationVerifier
	Sessions             SessionService
	Progress             ProgressService
	Plans                PlanProvider
	Bootstrap            BootstrapProvider
	BootReports          BootReporter
	RateLimiter          *rate.Limiter
	Now                  func() time.Time
}

type Handler struct {
	config Config
	mux    *http.ServeMux
}

func NewHandler(config Config) *Handler {
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.RateLimiter == nil {
		config.RateLimiter = rate.NewLimiter(rate.Limit(20), 40)
	}
	handler := &Handler{config: config, mux: http.NewServeMux()}
	handler.mux.HandleFunc("POST /v1/agent/register", handler.register)
	handler.mux.HandleFunc("GET /v1/operations/{uid}/plan", handler.plan)
	handler.mux.HandleFunc("POST /v1/operations/{uid}/progress", handler.progress)
	handler.mux.HandleFunc("GET /v1/operations/{uid}/bootstrap", handler.bootstrap)
	handler.mux.HandleFunc("POST /v1/operations/{uid}/boot-report", handler.bootReport)
	return handler
}

func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	// Agent APIはcredentialを運ぶため、平文HTTPへのredirectや処理継続を行わない。
	if request.TLS == nil {
		handler.writeError(writer, http.StatusUpgradeRequired, "https_required", "HTTPS is required")
		return
	}
	if !handler.config.RateLimiter.Allow() {
		handler.writeError(writer, http.StatusTooManyRequests, "rate_limited", "Rate limit exceeded")
		return
	}
	handler.mux.ServeHTTP(writer, request)
}

func (handler *Handler) register(writer http.ResponseWriter, request *http.Request) {
	if !handler.dependenciesAvailable(
		writer,
		handler.config.Operations,
		handler.config.RegistrationVerifier,
		handler.config.Sessions,
	) {
		return
	}
	var body agentprotocol.RegisterRequest
	if !handler.decodeRequest(writer, request, &body) {
		return
	}
	if body.APIVersion != agentprotocol.APIVersion ||
		body.OperationUID == "" ||
		body.HostUID == "" ||
		body.AgentInstanceID == "" {
		handler.writeError(writer, http.StatusUnprocessableEntity, "invalid_request", "Registration request is invalid")
		return
	}
	key, operation, err := handler.config.Operations.Resolve(request.Context(), body.OperationUID)
	if err != nil || operation == nil || operation.Spec.OperationID != body.OperationUID {
		handler.notFound(writer)
		return
	}
	if err := handler.config.RegistrationVerifier.Verify(
		request.Context(),
		operation,
		request.Header.Get("Authorization"),
		body,
	); err != nil {
		handler.unauthorized(writer)
		return
	}
	token, expiresAt, err := handler.config.Sessions.Issue(
		request.Context(),
		key,
		body.HostUID,
		body.OperationUID,
		handler.config.Now(),
	)
	if err != nil {
		handler.internalError(writer)
		return
	}
	handler.writeJSON(writer, http.StatusOK, agentprotocol.RegisterResponse{
		APIVersion:   agentprotocol.APIVersion,
		SessionToken: token.BearerValue(),
		ExpiresAt:    expiresAt,
		PlanDigest:   operation.Spec.PlanDigest,
	})
}

func (handler *Handler) plan(writer http.ResponseWriter, request *http.Request) {
	if !handler.dependenciesAvailable(writer, handler.config.Operations, handler.config.Sessions, handler.config.Plans) {
		return
	}
	key, operation, ok := handler.authorizeOperation(writer, request)
	if !ok {
		return
	}
	signedPlan, err := handler.config.Plans.GetPlan(request.Context(), key)
	if err != nil {
		handler.notFound(writer)
		return
	}
	validated, err := agentprotocol.ValidatePlan(signedPlan.Plan)
	if err != nil {
		handler.internalError(writer)
		return
	}
	planDigest, err := validated.Digest()
	if err != nil || planDigest.String() != operation.Spec.PlanDigest {
		handler.notFound(writer)
		return
	}
	handler.writeJSON(writer, http.StatusOK, signedPlan)
}

func (handler *Handler) progress(writer http.ResponseWriter, request *http.Request) {
	if !handler.dependenciesAvailable(
		writer,
		handler.config.Operations,
		handler.config.Sessions,
		handler.config.Progress,
		handler.config.Plans,
	) {
		return
	}
	key, operation, ok := handler.authorizeOperation(writer, request)
	if !ok {
		return
	}
	var body agentprotocol.ProgressRequest
	if !handler.decodeRequest(writer, request, &body) {
		return
	}
	if body.APIVersion != agentprotocol.APIVersion ||
		body.OperationUID != operation.Spec.OperationID ||
		body.PlanDigest != operation.Spec.PlanDigest {
		handler.notFound(writer)
		return
	}
	if body.CompletedStep == "" {
		handler.writeError(writer, http.StatusUnprocessableEntity, "invalid_request", "completedStep is required")
		return
	}
	signedPlan, err := handler.config.Plans.GetPlan(request.Context(), key)
	if err != nil {
		handler.notFound(writer)
		return
	}
	validatedPlan, err := agentprotocol.ValidatePlan(signedPlan.Plan)
	if err != nil {
		handler.internalError(writer)
		return
	}
	planDigest, err := validatedPlan.Digest()
	if err != nil || planDigest.String() != operation.Spec.PlanDigest {
		handler.notFound(writer)
		return
	}
	if !planContainsStep(signedPlan.Plan, body.CompletedStep) {
		handler.writeError(writer, http.StatusUnprocessableEntity, "unknown_step", "Completed step is not present in the Plan")
		return
	}
	result, err := handler.config.Progress.Report(
		request.Context(),
		key,
		body.OperationUID,
		body.PlanDigest,
		body.AgentSequence,
		body.CompletedStep,
	)
	if err != nil {
		if errors.Is(err, k8sagentprogress.ErrOperationNotFound) {
			handler.notFound(writer)
			return
		}
		handler.internalError(writer)
		return
	}
	switch result.Decision {
	case agentprogressdomain.DecisionGap, agentprogressdomain.DecisionInvalid:
		handler.writeJSON(writer, http.StatusConflict, progressResponse(result))
	case agentprogressdomain.DecisionApply, agentprogressdomain.DecisionDuplicate:
		handler.writeJSON(writer, http.StatusOK, progressResponse(result))
	default:
		handler.internalError(writer)
	}
}

func planContainsStep(plan agentprotocol.Plan, completedStep string) bool {
	for _, step := range plan.Steps {
		if step.Name == completedStep {
			return true
		}
	}
	return false
}

func (handler *Handler) bootstrap(writer http.ResponseWriter, request *http.Request) {
	if !handler.dependenciesAvailable(writer, handler.config.Operations, handler.config.Sessions, handler.config.Bootstrap) {
		return
	}
	key, operation, token, ok := handler.operationAndToken(writer, request)
	if !ok {
		return
	}
	bundle, err := handler.config.Bootstrap.GetBootstrapBundle(request.Context(), key)
	if errors.Is(err, agentprotocol.ErrUnsupportedBootstrapFormat) {
		handler.writeError(writer, http.StatusUnprocessableEntity, "unsupported_format", "Bootstrap format is not supported")
		return
	}
	if errors.Is(err, agentprotocol.ErrBootstrapTooLarge) {
		handler.writeError(writer, http.StatusRequestEntityTooLarge, "response_too_large", "Bootstrap response exceeds 16 MiB")
		return
	}
	validationErr := agentprotocol.ValidateBootstrapBundle(bundle)
	if errors.Is(validationErr, agentprotocol.ErrUnsupportedBootstrapFormat) {
		handler.writeError(writer, http.StatusUnprocessableEntity, "unsupported_format", "Bootstrap format is not supported")
		return
	}
	if errors.Is(validationErr, agentprotocol.ErrBootstrapTooLarge) {
		handler.writeError(writer, http.StatusRequestEntityTooLarge, "response_too_large", "Bootstrap response exceeds 16 MiB")
		return
	}
	if err != nil ||
		bundle.OperationUID != operation.Spec.OperationID ||
		validationErr != nil {
		handler.notFound(writer)
		return
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		handler.internalError(writer)
		return
	}
	if len(encoded) > agentprotocol.MaxBootstrapBodyBytes {
		handler.writeError(writer, http.StatusRequestEntityTooLarge, "response_too_large", "Bootstrap response exceeds 16 MiB")
		return
	}

	// Bundleの準備後にtokenを原子的にclaimする。以後の通信断では再利用を許可しない。
	if err := handler.config.Sessions.ClaimBootstrap(
		request.Context(),
		key,
		token,
		string(operation.Spec.HostRef.UID),
		operation.Spec.OperationID,
		handler.config.Now(),
	); err != nil {
		handler.unauthorized(writer)
		return
	}
	handler.writeEncodedJSON(writer, http.StatusOK, encoded)
}

func (handler *Handler) bootReport(writer http.ResponseWriter, request *http.Request) {
	if !handler.dependenciesAvailable(writer, handler.config.Operations, handler.config.Sessions, handler.config.BootReports) {
		return
	}
	key, operation, ok := handler.authorizeOperation(writer, request)
	if !ok {
		return
	}
	var body agentprotocol.BootReportRequest
	if !handler.decodeRequest(writer, request, &body) {
		return
	}
	if body.APIVersion != agentprotocol.APIVersion ||
		body.OperationUID != operation.Spec.OperationID ||
		body.PlanDigest != operation.Spec.PlanDigest {
		handler.notFound(writer)
		return
	}
	if err := agentprotocol.ValidateBootReport(body); err != nil {
		handler.writeError(writer, http.StatusUnprocessableEntity, "invalid_request", "Boot report is invalid")
		return
	}
	if err := handler.config.BootReports.ReportBoot(
		request.Context(),
		key,
		body,
		metav1.NewTime(handler.config.Now()),
	); err != nil {
		if errors.Is(err, k8sbootreport.ErrOperationNotFound) {
			handler.notFound(writer)
			return
		}
		if errors.Is(err, k8sbootreport.ErrReportConflict) {
			handler.writeError(writer, http.StatusConflict, "boot_report_conflict", "Boot report conflicts with operation state")
			return
		}
		handler.internalError(writer)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (handler *Handler) authorizeOperation(
	writer http.ResponseWriter,
	request *http.Request,
) (client.ObjectKey, *infrastructurev1beta1.TartHostOperation, bool) {
	key, operation, token, ok := handler.operationAndToken(writer, request)
	if !ok {
		return client.ObjectKey{}, nil, false
	}
	if err := handler.config.Sessions.Authenticate(
		request.Context(),
		key,
		token,
		string(operation.Spec.HostRef.UID),
		operation.Spec.OperationID,
		handler.config.Now(),
	); err != nil {
		handler.unauthorized(writer)
		return client.ObjectKey{}, nil, false
	}
	return key, operation, true
}

func (handler *Handler) operationAndToken(
	writer http.ResponseWriter,
	request *http.Request,
) (client.ObjectKey, *infrastructurev1beta1.TartHostOperation, string, bool) {
	operationUID := request.PathValue("uid")
	key, operation, err := handler.config.Operations.Resolve(request.Context(), operationUID)
	if err != nil || operation == nil || operation.Spec.OperationID != operationUID {
		handler.notFound(writer)
		return client.ObjectKey{}, nil, "", false
	}
	token, ok := bearerToken(request.Header.Get("Authorization"))
	if !ok {
		handler.unauthorized(writer)
		return client.ObjectKey{}, nil, "", false
	}
	return key, operation, token, true
}

func (handler *Handler) decodeRequest(writer http.ResponseWriter, request *http.Request, target any) bool {
	err := agentprotocol.DecodeRequest(request.Body, target)
	if errors.Is(err, agentprotocol.ErrRequestTooLarge) {
		handler.writeError(writer, http.StatusRequestEntityTooLarge, "request_too_large", "Request body exceeds 1 MiB")
		return false
	}
	if err != nil {
		handler.writeError(writer, http.StatusBadRequest, "invalid_json", "Request body is invalid")
		return false
	}
	return true
}

func (handler *Handler) dependenciesAvailable(writer http.ResponseWriter, dependencies ...any) bool {
	for _, dependency := range dependencies {
		if dependency == nil {
			handler.internalError(writer)
			return false
		}
	}
	return true
}

func (handler *Handler) notFound(writer http.ResponseWriter) {
	handler.writeError(writer, http.StatusNotFound, "operation_not_found", "Operation or plan was not found")
}

func (handler *Handler) unauthorized(writer http.ResponseWriter) {
	handler.writeError(writer, http.StatusUnauthorized, "unauthorized", "Authentication failed")
}

func (handler *Handler) internalError(writer http.ResponseWriter) {
	handler.writeError(writer, http.StatusInternalServerError, "internal_error", "Internal server error")
}

func (handler *Handler) writeError(writer http.ResponseWriter, status int, code, message string) {
	handler.writeJSON(writer, status, agentprotocol.ErrorResponse{
		APIVersion: agentprotocol.APIVersion,
		Code:       code,
		Message:    message,
	})
}

func (handler *Handler) writeJSON(writer http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		http.Error(writer, "Internal server error", http.StatusInternalServerError)
		return
	}
	handler.writeEncodedJSON(writer, status, encoded)
}

func (*Handler) writeEncodedJSON(writer http.ResponseWriter, status int, encoded []byte) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	if _, err := writer.Write(encoded); err != nil {
		// Response開始後はstatusを変更できない。credentialやbodyを含めずに上位serverへ返す手段もない。
		return
	}
}

func bearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(header, " ")
	return token, found && strings.EqualFold(scheme, "Bearer") && token != ""
}

func progressResponse(result k8sagentprogress.Result) agentprotocol.ProgressResponse {
	return agentprotocol.ProgressResponse{
		APIVersion:     agentprotocol.APIVersion,
		AgentSequence:  result.AgentSequence,
		CompletedSteps: result.CompletedSteps,
	}
}

var _ http.Handler = (*Handler)(nil)
