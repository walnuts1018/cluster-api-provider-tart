package ipxe

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
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
	"golang.org/x/time/rate"
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
	metadataLimiter   *rate.Limiter
}

type HandlerConfig struct {
	AssetsRoot        string
	BaseURL           string
	MetadataLimiter   *rate.Limiter
	BootstrapTokenSvc applicationbootstraptoken.Service
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
	registerMetadataRoutes(e, cl, config.MetadataLimiter, config.BootstrapTokenSvc)
	if config.AssetsRoot != "" {
		e.Static("/assets", config.AssetsRoot)
	}
	return e
}

func registerMetadataRoutes(e *echo.Echo, cl client.Client, limiter *rate.Limiter, svc applicationbootstraptoken.Service) {
	register := func(path string, handler func(*echo.Context) error) {
		if limiter != nil {
			e.GET(path, func(c *echo.Context) error {
				if !limiter.Allow() {
					return c.String(http.StatusTooManyRequests, "rate limit exceeded")
				}
				return handler(c)
			})
			return
		}
		e.GET(path, handler)
	}

	register("/metadata/:namespace/:name", func(c *echo.Context) error { return handleMetadata(c, cl, svc) })
	register("/metadata/:namespace/:name/talos/:token", func(c *echo.Context) error { return handleMetadata(c, cl, svc) })
	register("/metadata/:namespace/:name/nocloud/:token/meta-data", func(c *echo.Context) error { return serveNoCloudMetaData(c, cl, svc) })
	register("/metadata/:namespace/:name/nocloud/:token/user-data", func(c *echo.Context) error {
		return serveBootstrapData(c, cl, "text/cloud-config; charset=utf-8", true, c.Param("token"), true, svc)
	})
	register("/metadata/:namespace/:name/nocloud/:token/vendor-data", func(c *echo.Context) error { return serveNoCloudVendorData(c, cl, svc) })
	register("/metadata/:namespace/:name/preseed.cfg", func(c *echo.Context) error {
		return serveBootstrapData(c, cl, "text/plain; charset=utf-8", true, c.QueryParam("token"), true, svc)
	})
	register("/metadata/:namespace/:name/preseed/:token/preseed.cfg", func(c *echo.Context) error {
		return serveBootstrapData(c, cl, "text/plain; charset=utf-8", true, c.Param("token"), true, svc)
	})
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
		script := "#!ipxe\npoweroff\n"
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
		return generateIPXEScript(c, cl, &machine, svc, baseURL)
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

func generateIPXEScript(c *echo.Context, cl client.Client, machine *infrastructurev1alpha1.TartMachine, svc applicationbootstraptoken.Service, baseURL string) (string, error) {
	serverURL := baseURL

	var sb strings.Builder
	sb.WriteString("#!ipxe\n")

	bootstrapParams, err := buildBootstrapKernelParams(c.Request().Context(), cl, serverURL, machine, svc)
	if err != nil {
		return "", err
	}
	paramsList := append([]string{}, machine.Spec.KernelParams...)
	paramsList = append(paramsList, bootstrapParams...)
	params := strings.Join(paramsList, " ")

	if params == "" {
		fmt.Fprintf(&sb, "kernel %s\n", machine.Spec.Image)
	} else {
		fmt.Fprintf(&sb, "kernel %s %s\n", machine.Spec.Image, params)
	}
	if machine.Spec.Initrd != "" {
		fmt.Fprintf(&sb, "initrd %s\n", machine.Spec.Initrd)
	}
	sb.WriteString("boot\n")

	return sb.String(), nil
}

func bootstrapFormat(machine *infrastructurev1alpha1.TartMachine) infrastructurev1alpha1.TartMachineBootstrapFormat {
	if machine.Spec.Bootstrap.Format == "" {
		return infrastructurev1alpha1.TartMachineBootstrapFormatNoCloud
	}
	return machine.Spec.Bootstrap.Format
}

func buildBootstrapKernelParams(ctx context.Context, _ client.Client, serverURL string, machine *infrastructurev1alpha1.TartMachine, svc applicationbootstraptoken.Service) ([]string, error) {
	token, exists, err := svc.Get(ctx, machine)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	switch bootstrapFormat(machine) {
	case infrastructurev1alpha1.TartMachineBootstrapFormatTalos:
		return []string{"talos.config=" + buildMetadataURL(serverURL, machine, token.String())}, nil
	case infrastructurev1alpha1.TartMachineBootstrapFormatNoCloud:
		return []string{"ds=nocloud-net;s=" + buildNoCloudSeedURL(serverURL, machine, token.String())}, nil
	case infrastructurev1alpha1.TartMachineBootstrapFormatPreseed:
		return []string{"auto=true", "priority=critical", "url=" + buildPreseedURL(serverURL, machine, token.String())}, nil
	case infrastructurev1alpha1.TartMachineBootstrapFormatRaw:
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported bootstrap format: %s", machine.Spec.Bootstrap.Format)
	}
}

func buildMetadataURL(serverURL string, machine *infrastructurev1alpha1.TartMachine, token string) string {
	metadataPath := fmt.Sprintf("/metadata/%s/%s/talos/%s", url.PathEscape(machine.Namespace), url.PathEscape(machine.Name), url.PathEscape(token))
	return fmt.Sprintf("%s%s", strings.TrimSuffix(serverURL, "/"), metadataPath)
}

func buildNoCloudSeedURL(serverURL string, machine *infrastructurev1alpha1.TartMachine, token string) string {
	metadataPath := fmt.Sprintf("/metadata/%s/%s/nocloud/%s/", url.PathEscape(machine.Namespace), url.PathEscape(machine.Name), url.PathEscape(token))
	return fmt.Sprintf("%s%s", strings.TrimSuffix(serverURL, "/"), metadataPath)
}

func buildPreseedURL(serverURL string, machine *infrastructurev1alpha1.TartMachine, token string) string {
	metadataPath := fmt.Sprintf("/metadata/%s/%s/preseed/%s/preseed.cfg", url.PathEscape(machine.Namespace), url.PathEscape(machine.Name), url.PathEscape(token))
	return fmt.Sprintf("%s%s", strings.TrimSuffix(serverURL, "/"), metadataPath)
}

func handleMetadata(c *echo.Context, cl client.Client, svc applicationbootstraptoken.Service) error {
	token := c.Param("token")
	if token == "" {
		token = c.QueryParam("token")
	}
	return serveBootstrapData(c, cl, "application/octet-stream", true, token, true, svc)
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

func serveBootstrapData(c *echo.Context, cl client.Client, contentType string, consumeToken bool, providedToken string, requireLiveToken bool, svc applicationbootstraptoken.Service) error {
	ctx, span := telemetry.Tracer.Start(c.Request().Context(), "Metadata.Get")
	defer span.End()

	span.SetAttributes(
		attribute.String("metadata.namespace", c.Param("namespace")),
		attribute.String("metadata.name", c.Param("name")),
	)

	machine, err := validateMetadataRequest(c, cl, providedToken, requireLiveToken, svc)
	if err != nil {
		if httpErr, ok := err.(*echo.HTTPError); ok {
			span.SetStatus(codes.Error, httpErr.Message)
			return c.String(httpErr.Code, httpErr.Message)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return c.String(http.StatusInternalServerError, "failed to get TartMachine")
	}

	secretName, err := bootstrapDataSecretName(ctx, cl, machine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			span.SetStatus(codes.Error, "owner not found")
			return c.String(http.StatusNotFound, "bootstrap secret owner Machine not found")
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return c.String(http.StatusPreconditionFailed, err.Error())
	}

	var secret corev1.Secret
	if err := cl.Get(ctx, client.ObjectKey{
		Namespace: machine.Namespace,
		Name:      secretName,
	}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			span.SetStatus(codes.Error, "secret not found")
			return c.String(http.StatusNotFound, "bootstrap secret not found")
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return c.String(http.StatusInternalServerError, "failed to get bootstrap secret")
	}

	data, ok := secret.Data["value"]
	if !ok {
		span.SetStatus(codes.Error, "secret missing value key")
		return c.String(http.StatusPreconditionFailed, "bootstrap secret does not contain value key")
	}

	if consumeToken {
		if err := cl.Get(ctx, client.ObjectKey{
			Namespace: c.Param("namespace"),
			Name:      c.Param("name"),
		}, machine); err != nil {
			if apierrors.IsNotFound(err) {
				span.SetStatus(codes.Error, "machine not found on re-fetch")
				return c.String(http.StatusNotFound, "TartMachine not found")
			}
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return c.String(http.StatusInternalServerError, "failed to get TartMachine")
		}

		expectedToken, exists, err := svc.Get(ctx, machine)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return c.String(http.StatusInternalServerError, "failed to get bootstrap token")
		}
		if !exists || subtle.ConstantTimeCompare([]byte(providedToken), []byte(expectedToken.String())) != 1 {
			span.SetStatus(codes.Error, "token consumed")
			return c.String(http.StatusForbidden, "bootstrap token has already been consumed")
		}

		if err := consumeBootstrapToken(ctx, cl, machine, providedToken, svc); err != nil {
			if apierrors.IsConflict(err) {
				span.SetStatus(codes.Error, "token consumed by conflict")
				return c.String(http.StatusForbidden, "bootstrap token has already been consumed")
			}
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return c.String(http.StatusInternalServerError, "failed to consume bootstrap token")
		}
	}

	span.SetStatus(codes.Ok, "bootstrap token consumed")
	return c.Blob(http.StatusOK, contentType, data)
}

func serveNoCloudMetaData(c *echo.Context, cl client.Client, svc applicationbootstraptoken.Service) error {
	_, span := telemetry.Tracer.Start(c.Request().Context(), "Metadata.Get")
	defer span.End()

	span.SetAttributes(
		attribute.String("metadata.namespace", c.Param("namespace")),
		attribute.String("metadata.name", c.Param("name")),
	)

	machine, err := validateNoCloudMetadataRequest(c, cl, svc)
	if err != nil {
		if httpErr, ok := err.(*echo.HTTPError); ok {
			span.SetStatus(codes.Error, httpErr.Message)
			return c.String(httpErr.Code, httpErr.Message)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return c.String(http.StatusInternalServerError, "failed to get TartMachine")
	}

	body := fmt.Sprintf(
		"instance-id: %s-%s\nlocal-hostname: %s\n",
		machine.Namespace,
		machine.Name,
		machine.Name,
	)
	span.SetStatus(codes.Ok, "NoCloud meta-data generated")
	return c.Blob(http.StatusOK, "text/yaml; charset=utf-8", []byte(body))
}

func serveNoCloudVendorData(c *echo.Context, cl client.Client, svc applicationbootstraptoken.Service) error {
	_, span := telemetry.Tracer.Start(c.Request().Context(), "Metadata.Get")
	defer span.End()

	span.SetAttributes(
		attribute.String("metadata.namespace", c.Param("namespace")),
		attribute.String("metadata.name", c.Param("name")),
	)

	if _, err := validateNoCloudMetadataRequest(c, cl, svc); err != nil {
		if httpErr, ok := err.(*echo.HTTPError); ok {
			span.SetStatus(codes.Error, httpErr.Message)
			return c.String(httpErr.Code, httpErr.Message)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return c.String(http.StatusInternalServerError, "failed to get TartMachine")
	}

	span.SetStatus(codes.Ok, "NoCloud vendor-data generated")
	return c.Blob(http.StatusOK, "text/cloud-config; charset=utf-8", []byte("#cloud-config\n{}\n"))
}

func validateNoCloudMetadataRequest(c *echo.Context, cl client.Client, svc applicationbootstraptoken.Service) (*infrastructurev1alpha1.TartMachine, error) {
	pathToken := c.Param("token")
	if pathToken == "" {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "token is required")
	}

	ctx := c.Request().Context()
	var machine infrastructurev1alpha1.TartMachine
	if err := cl.Get(ctx, client.ObjectKey{
		Namespace: c.Param("namespace"),
		Name:      c.Param("name"),
	}, &machine); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "TartMachine not found")
		}
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to get TartMachine")
	}

	expectedToken, exists, err := svc.Get(ctx, &machine)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to get bootstrap token")
	}
	if exists {
		if subtle.ConstantTimeCompare([]byte(pathToken), []byte(expectedToken.String())) != 1 {
			return nil, echo.NewHTTPError(http.StatusUnauthorized, "invalid or missing token")
		}
		if machine.Status.TokenExpiresAt != nil {
			now := metav1.NewTime(time.Now())
			if machine.Status.TokenExpiresAt.Before(&now) {
				return nil, echo.NewHTTPError(http.StatusNotFound, "token has expired")
			}
		}
		return &machine, nil
	}

	if machine.Status.ConsumedBootstrapTokenHash == "" {
		return nil, echo.NewHTTPError(http.StatusForbidden, "bootstrap token has already been consumed")
	}
	if subtle.ConstantTimeCompare([]byte(bootstrapTokenHash(pathToken)), []byte(machine.Status.ConsumedBootstrapTokenHash)) != 1 {
		return nil, echo.NewHTTPError(http.StatusForbidden, "bootstrap token has already been consumed")
	}
	return &machine, nil
}

func bootstrapDataSecretName(ctx context.Context, cl client.Client, machine *infrastructurev1alpha1.TartMachine) (string, error) {
	gvk, name := ownerMachineReference(machine)
	if name == "" {
		gvk = schema.GroupVersionKind{Group: "cluster.x-k8s.io", Version: "v1beta1", Kind: "Machine"}
		name = machine.Name
	}

	var capiMachine unstructured.Unstructured
	capiMachine.SetGroupVersionKind(gvk)
	if err := cl.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: name}, &capiMachine); err != nil {
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
		metadataLimiter:   rate.NewLimiter(rate.Every(100*time.Millisecond), 5),
	}
}

func (s *Server) Addr() string {
	return s.addr
}

func (s *Server) Start(ctx context.Context) error {
	log := crlog.FromContext(ctx).WithName("ipxe")

	server := &http.Server{
		Addr:              s.addr,
		Handler:           NewHandler(s.client, HandlerConfig{AssetsRoot: s.assetsRoot, BaseURL: s.baseURL, MetadataLimiter: s.metadataLimiter, BootstrapTokenSvc: s.bootstrapTokenSvc}),
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
