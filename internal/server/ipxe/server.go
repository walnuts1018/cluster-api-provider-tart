package ipxe

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	echootel "github.com/labstack/echo-opentelemetry"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	applicationbootstraptoken "github.com/walnuts1018/cluster-api-provider-tart/internal/application/bootstraptoken"
	machinedomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machine"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
)

type Server struct {
	client            client.Client
	bootstrapTokenSvc applicationbootstraptoken.Service
	addr              string
	assetsRoot        string
	baseURL           string
}

type HandlerConfig struct {
	AssetsRoot        string
	BaseURL           string
	BootstrapTokenSvc applicationbootstraptoken.Service
}

const (
	agentKernelPath = "/assets/agent/vmlinuz"
	agentInitrdPath = "/assets/agent/initrd"
)

type provisioningConfig struct {
	TargetDevice  string                      `json:"targetDevice"`
	ImageURL      string                      `json:"imageUrl"`
	RepairGPT     bool                        `json:"repairGPT"`
	CIDATASizeMiB int                         `json:"cidataSizeMiB"`
	CompleteURL   string                      `json:"completeUrl"`
	Bootstrap     provisioningBootstrapConfig `json:"bootstrap"`
}

type provisioningBootstrapConfig struct {
	Format      infrastructurev1alpha1.TartMachineBootstrapFormat `json:"format"`
	UserData    string                                            `json:"userData,omitempty"`
	MetaData    string                                            `json:"metaData,omitempty"`
	VendorData  string                                            `json:"vendorData,omitempty"`
	TalosConfig string                                            `json:"talosConfig,omitempty"`
}

func NewHandler(cl client.Client, config HandlerConfig) http.Handler {
	e := echo.New()
	e.Use(middleware.RequestID())
	e.Use(middleware.Recover())

	e.Use(echootel.NewMiddlewareWithConfig(echootel.Config{
		ServerName:     "tart",
		TracerProvider: otel.GetTracerProvider(),
		MeterProvider:  otel.GetMeterProvider(),
	}))

	e.GET("/livez", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	e.GET("/readyz", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	e.GET("/ipxe", func(c *echo.Context) error { return handleIPXE(c, cl, config.BootstrapTokenSvc, config.BaseURL) })
	e.GET("/provisioning/:namespace/:name/config/:token", func(c *echo.Context) error {
		return handleProvisioningConfig(c, cl, config.BootstrapTokenSvc, config.BaseURL)
	})
	e.POST("/provisioning/:namespace/:name/complete/:token", func(c *echo.Context) error {
		return handleProvisioningComplete(c, cl, config.BootstrapTokenSvc)
	})
	if config.AssetsRoot != "" {
		e.Static("/assets", config.AssetsRoot)
	}
	return e
}

func NormalizeMAC(mac string) (string, error) {
	hw, err := net.ParseMAC(mac)
	if err != nil {
		return "", err
	}
	return hw.String(), nil
}

func handleIPXE(c *echo.Context, cl client.Client, svc applicationbootstraptoken.Service, baseURL string) error {
	ctx, span := telemetry.Tracer.Start(c.Request().Context(), "IPXE.Get")
	defer span.End()

	mac := c.QueryParam("mac")
	if mac == "" {
		return c.String(http.StatusBadRequest, "mac query parameter is required")
	}
	normalizedMAC, err := NormalizeMAC(mac)
	if err != nil {
		return c.String(http.StatusBadRequest, fmt.Sprintf("invalid mac address format: %s", mac))
	}

	span.SetAttributes(attribute.String("ipxe.mac", normalizedMAC))

	targetHost, err := findHostByMAC(ctx, cl, normalizedMAC)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return c.String(http.StatusInternalServerError, err.Error())
	}
	if targetHost == nil {
		span.SetStatus(codes.Error, "host not found")
		script := "#!ipxe\nexit\n"
		return c.Blob(http.StatusOK, "text/plain; charset=utf-8", []byte(script))
	}

	script, err := generateIPXEScriptByHostState(c, cl, targetHost, svc, baseURL)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return c.String(http.StatusInternalServerError, "failed to generate iPXE script")
	}
	span.SetStatus(codes.Ok, "IPXE script generated")
	return c.Blob(http.StatusOK, "text/plain; charset=utf-8", []byte(script))
}

func generateIPXEScriptByHostState(c *echo.Context, cl client.Client, host *infrastructurev1alpha1.TartHost, svc applicationbootstraptoken.Service, baseURL string) (string, error) {
	switch host.Status.State {
	case infrastructurev1alpha1.TartHostStateProvisioning:
		if host.Status.MachineRef == nil {
			return "", fmt.Errorf("host is in provisioning state but has no machine ref")
		}
		var machine infrastructurev1alpha1.TartMachine
		if err := cl.Get(c.Request().Context(), client.ObjectKey{
			Namespace: host.Status.MachineRef.Namespace,
			Name:      host.Status.MachineRef.Name,
		}, &machine); err != nil {
			if apierrors.IsNotFound(err) {
				return "", fmt.Errorf("assigned TartMachine not found")
			}
			return "", fmt.Errorf("failed to get TartMachine: %w", err)
		}
		return generateIPXEScript(c, cl, host, &machine, svc, baseURL)
	case infrastructurev1alpha1.TartHostStateProvisioned:
		return "#!ipxe\nexit\n", nil
	case infrastructurev1alpha1.TartHostStateAvailable:
		return "#!ipxe\npoweroff\n", nil
	case infrastructurev1alpha1.TartHostStateReserved:
		return "#!ipxe\nsleep 60\npoweroff\n", nil
	default:
		return "#!ipxe\nsleep 60\npoweroff\n", nil
	}
}

func findHostByMAC(ctx context.Context, cl client.Client, normalizedMAC string) (*infrastructurev1alpha1.TartHost, error) {
	var bootHosts infrastructurev1alpha1.TartHostList
	if err := cl.List(ctx, &bootHosts, client.MatchingFields{"spec.bootMACAddress": normalizedMAC}); err != nil {
		return nil, fmt.Errorf("failed to list hosts by bootMACAddress")
	}
	if len(bootHosts.Items) > 0 {
		return &bootHosts.Items[0], nil
	}

	var hosts infrastructurev1alpha1.TartHostList
	if err := cl.List(ctx, &hosts, client.MatchingFields{"spec.macAddress": normalizedMAC}); err != nil {
		return nil, fmt.Errorf("failed to list hosts by macAddress")
	}
	if len(hosts.Items) > 0 {
		return &hosts.Items[0], nil
	}
	return nil, nil
}

func generateIPXEScript(c *echo.Context, _ client.Client, host *infrastructurev1alpha1.TartHost, machine *infrastructurev1alpha1.TartMachine, svc applicationbootstraptoken.Service, baseURL string) (string, error) {
	serverURL := baseURL

	var sb strings.Builder
	sb.WriteString("#!ipxe\n")

	token, exists, err := svc.Get(c.Request().Context(), machine)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", nil
	}

	paramsList := []string{
		"initrd=agent-initrd",
		"tart.config=" + buildProvisioningConfigURL(serverURL, machine, token.String()),
	}
	paramsList = append(paramsList, machine.Spec.KernelParams...)
	if host.Spec.Provisioning.Device == "" {
		paramsList = append(paramsList, "tart.missing_target_device=1")
	}

	fmt.Fprintf(&sb, "kernel %s %s\n", assetURL(serverURL, agentKernelPath), strings.Join(paramsList, " "))
	fmt.Fprintf(&sb, "initrd --name agent-initrd %s\n", assetURL(serverURL, agentInitrdPath))
	sb.WriteString("boot\n")

	return sb.String(), nil
}

func bootstrapFormat(machine *infrastructurev1alpha1.TartMachine) infrastructurev1alpha1.TartMachineBootstrapFormat {
	if machine.Spec.Bootstrap.Format == "" {
		return infrastructurev1alpha1.TartMachineBootstrapFormatNoCloud
	}
	return machine.Spec.Bootstrap.Format
}

func buildProvisioningConfigURL(serverURL string, machine *infrastructurev1alpha1.TartMachine, token string) string {
	u, _ := url.JoinPath(serverURL, "provisioning", machine.Namespace, machine.Name, "config", token)
	return u
}

func buildProvisioningCompleteURL(serverURL string, machine *infrastructurev1alpha1.TartMachine, token string) string {
	u, _ := url.JoinPath(serverURL, "provisioning", machine.Namespace, machine.Name, "complete", token)
	return u
}

func assetURL(serverURL, value string) string {
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	u, _ := url.JoinPath(serverURL, value)
	return u
}

func handleProvisioningConfig(c *echo.Context, cl client.Client, svc applicationbootstraptoken.Service, baseURL string) error {
	ctx, span := telemetry.Tracer.Start(c.Request().Context(), "Provisioning.Config.Get")
	defer span.End()

	machine, err := validateMetadataRequest(c, cl, c.Param("token"), true, svc)
	if err != nil {
		if httpErr, ok := err.(*echo.HTTPError); ok {
			span.SetStatus(codes.Error, httpErr.Message)
			return c.String(httpErr.Code, httpErr.Message)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return c.String(http.StatusInternalServerError, "failed to validate provisioning token")
	}

	host, err := provisioningHost(ctx, cl, machine)
	if err != nil {
		if httpErr, ok := err.(*echo.HTTPError); ok {
			span.SetStatus(codes.Error, httpErr.Message)
			return c.String(httpErr.Code, httpErr.Message)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return c.String(http.StatusInternalServerError, "failed to get provisioning host")
	}

	bootstrapData, err := bootstrapSecretValue(ctx, cl, machine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			span.SetStatus(codes.Error, "bootstrap secret not found")
			return c.String(http.StatusNotFound, "bootstrap secret not found")
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return c.String(http.StatusPreconditionFailed, err.Error())
	}

	config := provisioningConfig{
		TargetDevice:  host.Spec.Provisioning.Device,
		ImageURL:      assetURL(baseURL, machine.Spec.Image),
		RepairGPT:     true,
		CIDATASizeMiB: 20,
		CompleteURL:   buildProvisioningCompleteURL(baseURL, machine, c.Param("token")),
		Bootstrap: provisioningBootstrapConfig{
			Format: effectiveBootstrapFormat(machine),
		},
	}
	switch config.Bootstrap.Format {
	case infrastructurev1alpha1.TartMachineBootstrapFormatTalos:
		config.Bootstrap.TalosConfig = string(bootstrapData)
	default:
		config.Bootstrap.UserData = string(bootstrapData)
		config.Bootstrap.MetaData = fmt.Sprintf("instance-id: %s-%s\nlocal-hostname: %s\n", machine.Namespace, machine.Name, machine.Name)
		config.Bootstrap.VendorData = "#cloud-config\n{}\n"
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return c.String(http.StatusInternalServerError, "failed to serialize provisioning config")
	}

	span.SetStatus(codes.Ok, "provisioning config generated")
	return c.Blob(http.StatusOK, "application/json; charset=utf-8", configJSON)
}

func handleProvisioningComplete(c *echo.Context, cl client.Client, svc applicationbootstraptoken.Service) error {
	ctx, span := telemetry.Tracer.Start(c.Request().Context(), "Provisioning.Complete")
	defer span.End()

	machine, err := validateMetadataRequest(c, cl, c.Param("token"), true, svc)
	if err != nil {
		if httpErr, ok := err.(*echo.HTTPError); ok {
			span.SetStatus(codes.Error, httpErr.Message)
			return c.String(httpErr.Code, httpErr.Message)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return c.String(http.StatusInternalServerError, "failed to validate provisioning token")
	}

	// ディスク書き込み、GPT修復、CIDATA注入が完了してから token を消費し、途中失敗で Ready にならないようにします。
	if err := consumeBootstrapToken(ctx, cl, machine, c.Param("token"), svc); err != nil {
		if apierrors.IsConflict(err) {
			span.SetStatus(codes.Error, "token consumed by conflict")
			return c.String(http.StatusForbidden, "bootstrap token has already been consumed")
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return c.String(http.StatusInternalServerError, "failed to complete provisioning")
	}

	span.SetStatus(codes.Ok, "provisioning completed")
	return c.NoContent(http.StatusNoContent)
}

func provisioningHost(ctx context.Context, cl client.Client, machine *infrastructurev1alpha1.TartMachine) (*infrastructurev1alpha1.TartHost, error) {
	if machine.Status.HostRef == nil {
		return nil, echo.NewHTTPError(http.StatusPreconditionFailed, "hostRef is not set")
	}
	var host infrastructurev1alpha1.TartHost
	if err := cl.Get(ctx, client.ObjectKey{
		Namespace: machine.Status.HostRef.Namespace,
		Name:      machine.Status.HostRef.Name,
	}, &host); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "TartHost not found")
		}
		return nil, err
	}
	if host.Spec.Provisioning.Device == "" {
		return nil, echo.NewHTTPError(http.StatusPreconditionFailed, "target provisioning device is not set")
	}
	return &host, nil
}

func bootstrapSecretValue(ctx context.Context, cl client.Client, machine *infrastructurev1alpha1.TartMachine) ([]byte, error) {
	var secretName string
	if machine.Status.BootstrapSecretName != "" {
		secretName = machine.Status.BootstrapSecretName
	} else {
		var err error
		secretName, err = bootstrapDataSecretName(ctx, cl, machine)
		if err != nil {
			return nil, err
		}
	}

	var secret corev1.Secret
	if err := cl.Get(ctx, client.ObjectKey{
		Namespace: machine.Namespace,
		Name:      secretName,
	}, &secret); err != nil {
		return nil, err
	}
	data, ok := secret.Data["value"]
	if !ok {
		return nil, fmt.Errorf("bootstrap secret does not contain value key")
	}
	return data, nil
}

func effectiveBootstrapFormat(machine *infrastructurev1alpha1.TartMachine) infrastructurev1alpha1.TartMachineBootstrapFormat {
	return bootstrapFormat(machine)
}

func validateMetadataRequest(c *echo.Context, cl client.Client, providedToken string, requireLiveToken bool, svc applicationbootstraptoken.Service) (*infrastructurev1alpha1.TartMachine, error) {
	ctx := c.Request().Context()

	if providedToken == "" {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "token is required")
	}

	var machine infrastructurev1alpha1.TartMachine
	if err := cl.Get(ctx, client.ObjectKey{
		Namespace: c.Param("namespace"),
		Name:      c.Param("name"),
	}, &machine); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "TartMachine not found")
		}
		return nil, fmt.Errorf("failed to get TartMachine: %w", err)
	}

	expectedToken, exists, err := svc.Get(ctx, &machine)
	if err != nil {
		return nil, fmt.Errorf("failed to get bootstrap token: %w", err)
	}
	if !exists {
		if requireLiveToken {
			return nil, echo.NewHTTPError(http.StatusPreconditionFailed, "bootstrap token is not set")
		}
		if machine.Status.ConsumedBootstrapTokenHash != "" {
			if subtle.ConstantTimeCompare([]byte(bootstrapTokenHash(providedToken)), []byte(machine.Status.ConsumedBootstrapTokenHash)) != 1 {
				return nil, echo.NewHTTPError(http.StatusForbidden, "bootstrap token has already been consumed")
			}
		}
		return &machine, nil
	}
	if subtle.ConstantTimeCompare([]byte(providedToken), []byte(expectedToken.String())) != 1 {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "invalid or missing token")
	}

	if requireLiveToken && machine.Status.TokenExpiresAt != nil {
		now := metav1.NewTime(time.Now())
		if machine.Status.TokenExpiresAt.Before(&now) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "token has expired")
		}
	}

	return &machine, nil
}

func bootstrapDataSecretName(ctx context.Context, cl client.Client, machine *infrastructurev1alpha1.TartMachine) (string, error) {
	gvk, name := ownerMachineReference(machine)
	if name == "" {
		gvk = schema.GroupVersionKind{Group: "cluster.x-k8s.io", Version: "v1beta2", Kind: "Machine"}
		name = machine.Name
	}

	var capiMachine unstructured.Unstructured
	capiMachine.SetGroupVersionKind(gvk)
	if err := cl.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: name}, &capiMachine); err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("machine %s/%s not found: %w", machine.Namespace, name, err)
		}
		return "", err
	}

	secretName, found, err := unstructured.NestedString(capiMachine.Object, "spec", "bootstrap", "dataSecretName")
	if err != nil {
		return "", fmt.Errorf("failed to read bootstrap dataSecretName: %w", err)
	}
	if !found || secretName == "" {
		return "", fmt.Errorf("bootstrap dataSecretName is not set")
	}
	return secretName, nil
}

func ownerMachineReference(machine *infrastructurev1alpha1.TartMachine) (schema.GroupVersionKind, string) {
	for _, owner := range machine.OwnerReferences {
		if owner.Kind != "Machine" {
			continue
		}
		gv, err := schema.ParseGroupVersion(owner.APIVersion)
		if err != nil {
			return schema.GroupVersionKind{}, owner.Name
		}
		if gv.Group != "cluster.x-k8s.io" {
			continue
		}
		return gv.WithKind(owner.Kind), owner.Name
	}
	return schema.GroupVersionKind{}, ""
}

func consumeBootstrapToken(ctx context.Context, cl client.Client, machine *infrastructurev1alpha1.TartMachine, providedToken string, svc applicationbootstraptoken.Service) error {
	original := machine.DeepCopy()
	status, err := machinedomain.BootstrapTokenConsumedStatus(machine, bootstrapTokenHash(providedToken))
	if err != nil {
		return err
	}
	machine.Status = status
	if err := cl.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return err
	}
	return svc.Delete(ctx, machine)
}

func bootstrapTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func NewServer(cl client.Client, svc applicationbootstraptoken.Service, addr, assetsRoot, baseURL string) *Server {
	return &Server{
		client:            cl,
		bootstrapTokenSvc: svc,
		addr:              addr,
		assetsRoot:        assetsRoot,
		baseURL:           baseURL,
	}
}

func (s *Server) Addr() string {
	return s.addr
}

func (s *Server) Start(ctx context.Context) error {
	log := crlog.FromContext(ctx).WithName("ipxe")

	server := &http.Server{
		Addr:              s.addr,
		Handler:           NewHandler(s.client, HandlerConfig{AssetsRoot: s.assetsRoot, BaseURL: s.baseURL, BootstrapTokenSvc: s.bootstrapTokenSvc}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("Starting iPXE HTTP server", "addr", s.addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		log.Info("Stopping iPXE HTTP server", "addr", s.addr)
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err, ok := <-errCh; ok && err != nil {
			return err
		}
		return nil
	case err, ok := <-errCh:
		if !ok {
			return nil
		}
		return err
	}
}

func (s *Server) NeedLeaderElection() bool {
	return false
}
