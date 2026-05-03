package ipxe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
)

type Server struct {
	client     client.Client
	addr       string
	assetsRoot string
}

type HandlerConfig struct {
	AssetsRoot string
}

func NewHandler(cl client.Client, config HandlerConfig) http.Handler {
	e := echo.New()
	e.Use(middleware.RequestID())
	e.Use(middleware.Recover())

	e.GET("/livez", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	e.GET("/readyz", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	e.GET("/ipxe", func(c *echo.Context) error { return handleIPXE(c, cl) })
	e.GET("/metadata/:namespace/:name", func(c *echo.Context) error { return handleMetadata(c, cl) })
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

func handleIPXE(c *echo.Context, cl client.Client) error {
	mac := c.QueryParam("mac")
	if mac == "" {
		return c.String(http.StatusBadRequest, "mac query parameter is required")
	}
	normalizedMAC, err := NormalizeMAC(mac)
	if err != nil {
		return c.String(http.StatusBadRequest, fmt.Sprintf("invalid mac address format: %s", mac))
	}

	ctx := c.Request().Context()

	targetHost, err := findHostByMAC(ctx, cl, normalizedMAC)
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}
	if targetHost == nil {
		return c.String(http.StatusNotFound, fmt.Sprintf("no TartHost found for MAC %s", normalizedMAC))
	}
	if targetHost.Status.MachineRef == nil {
		return c.String(http.StatusPreconditionFailed, "host is not assigned to any machine")
	}

	var machine infrastructurev1alpha1.TartMachine
	if err := cl.Get(ctx, client.ObjectKey{
		Namespace: targetHost.Status.MachineRef.Namespace,
		Name:      targetHost.Status.MachineRef.Name,
	}, &machine); err != nil {
		if apierrors.IsNotFound(err) {
			return c.String(http.StatusNotFound, "assigned TartMachine not found")
		}
		return c.String(http.StatusInternalServerError, "failed to get TartMachine")
	}

	script := generateIPXEScript(c, &machine)
	return c.Blob(http.StatusOK, "text/plain; charset=utf-8", []byte(script))
}

func findHostByMAC(ctx context.Context, cl client.Client, normalizedMAC string) (*infrastructurev1alpha1.TartHost, error) {
	var hosts infrastructurev1alpha1.TartHostList
	if err := cl.List(ctx, &hosts, client.MatchingFields{"spec.macAddress": normalizedMAC}); err != nil {
		return nil, fmt.Errorf("failed to list hosts by macAddress")
	}

	var bootHosts infrastructurev1alpha1.TartHostList
	if err := cl.List(ctx, &bootHosts, client.MatchingFields{"spec.bootMACAddress": normalizedMAC}); err != nil {
		return nil, fmt.Errorf("failed to list hosts by bootMACAddress")
	}

	if len(bootHosts.Items) > 0 {
		return &bootHosts.Items[0], nil
	}
	if len(hosts.Items) > 0 {
		return &hosts.Items[0], nil
	}
	return nil, nil
}

func generateIPXEScript(c *echo.Context, machine *infrastructurev1alpha1.TartMachine) string {
	serverURL := fmt.Sprintf("http://%s", c.Request().Host)

	var sb strings.Builder
	sb.WriteString("#!ipxe\n")

	params := strings.Join(machine.Spec.KernelParams, " ")
	metadataURL := buildMetadataURL(serverURL, machine)
	if metadataURL != "" {
		if params != "" {
			params += " "
		}
		params += "talos.config=" + metadataURL
	}

	if params == "" {
		fmt.Fprintf(&sb, "kernel %s\n", machine.Spec.Image)
	} else {
		fmt.Fprintf(&sb, "kernel %s %s\n", machine.Spec.Image, params)
	}
	if machine.Spec.Initrd != "" {
		fmt.Fprintf(&sb, "initrd %s\n", machine.Spec.Initrd)
	}
	sb.WriteString("boot\n")

	return sb.String()
}

func buildMetadataURL(serverURL string, machine *infrastructurev1alpha1.TartMachine) string {
	if machine.Status.BootstrapToken == "" {
		return ""
	}
	metadataPath := fmt.Sprintf("/metadata/%s/%s", url.PathEscape(machine.Namespace), url.PathEscape(machine.Name))
	return fmt.Sprintf("%s%s?token=%s", serverURL, metadataPath, url.QueryEscape(machine.Status.BootstrapToken))
}

func handleMetadata(c *echo.Context, cl client.Client) error {
	ctx := c.Request().Context()

	var machine infrastructurev1alpha1.TartMachine
	if err := cl.Get(ctx, client.ObjectKey{
		Namespace: c.Param("namespace"),
		Name:      c.Param("name"),
	}, &machine); err != nil {
		if apierrors.IsNotFound(err) {
			return c.String(http.StatusNotFound, "TartMachine not found")
		}
		return c.String(http.StatusInternalServerError, "failed to get TartMachine")
	}

	if machine.Status.BootstrapToken == "" {
		return c.String(http.StatusPreconditionFailed, "bootstrap token is not set")
	}

	providedToken := c.QueryParam("token")
	if providedToken != machine.Status.BootstrapToken {
		return c.String(http.StatusUnauthorized, "invalid or missing token")
	}

	if machine.Status.TokenExpiresAt != nil && machine.Status.TokenExpiresAt.Before(&metav1.Time{Time: time.Now()}) {
		return c.String(http.StatusNotFound, "token has expired")
	}

	secretName, err := bootstrapDataSecretName(ctx, cl, &machine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return c.String(http.StatusNotFound, "bootstrap secret owner Machine not found")
		}
		return c.String(http.StatusPreconditionFailed, err.Error())
	}

	var secret corev1.Secret
	if err := cl.Get(ctx, client.ObjectKey{
		Namespace: machine.Namespace,
		Name:      secretName,
	}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return c.String(http.StatusNotFound, "bootstrap secret not found")
		}
		return c.String(http.StatusInternalServerError, "failed to get bootstrap secret")
	}

	data, ok := secret.Data["value"]
	if !ok {
		return c.String(http.StatusPreconditionFailed, "bootstrap secret does not contain value key")
	}

	if err := consumeBootstrapToken(ctx, cl, &machine); err != nil {
		if apierrors.IsConflict(err) {
			return c.String(http.StatusForbidden, "bootstrap token has already been consumed")
		}
		return c.String(http.StatusInternalServerError, "failed to consume bootstrap token")
	}

	return c.Blob(http.StatusOK, "application/octet-stream", data)
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

func consumeBootstrapToken(ctx context.Context, cl client.Client, machine *infrastructurev1alpha1.TartMachine) error {
	machine.Status.BootstrapToken = ""
	machine.Status.TokenExpiresAt = nil
	return cl.Status().Update(ctx, machine)
}

func NewServer(cl client.Client, addr, assetsRoot string) *Server {
	return &Server{
		client:     cl,
		addr:       addr,
		assetsRoot: assetsRoot,
	}
}

func (s *Server) Addr() string {
	return s.addr
}

func (s *Server) Start(ctx context.Context) error {
	log := crlog.FromContext(ctx).WithName("ipxe")

	server := &http.Server{
		Addr:              s.addr,
		Handler:           NewHandler(s.client, HandlerConfig{AssetsRoot: s.assetsRoot}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("iPXE HTTP サーバーを起動します", "addr", s.addr)
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

		log.Info("iPXE HTTP サーバーを停止します", "addr", s.addr)
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
