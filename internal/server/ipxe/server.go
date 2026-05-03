package ipxe

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
)

type Server struct {
	client client.Client
	scheme *runtime.Scheme
	addr   string
}

func NewHandler(cl client.Client, scheme *runtime.Scheme) http.Handler {
	e := echo.New()
	e.Use(middleware.RequestID())
	e.Use(middleware.Recover())

	e.GET("/livez", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	e.GET("/readyz", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	e.GET("/ipxe", func(c *echo.Context) error {
		mac := c.QueryParam("mac")
		if mac == "" {
			return c.String(http.StatusBadRequest, "mac query parameter is required")
		}
		normalizedMAC := normalizeMAC(mac)

		ctx := c.Request().Context()
		var hosts infrastructurev1alpha1.TartHostList
		if err := cl.List(ctx, &hosts); err != nil {
			return c.String(http.StatusInternalServerError, "failed to list hosts")
		}

		var targetHost *infrastructurev1alpha1.TartHost
		for i := range hosts.Items {
			host := &hosts.Items[i]
			if normalizeMAC(host.Spec.MACAddress) == normalizedMAC ||
				(host.Spec.BootMACAddress != "" && normalizeMAC(host.Spec.BootMACAddress) == normalizedMAC) {
				targetHost = host
				break
			}
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
			return c.String(http.StatusInternalServerError, "failed to get TartMachine")
		}

		script := generateIPXEScript(c, &machine, targetHost)

		return c.Blob(http.StatusOK, "text/plain; charset=utf-8", []byte(script))
	})
	return e
}

func normalizeMAC(mac string) string {
	s := strings.ReplaceAll(mac, "-", ":")
	return strings.ToLower(s)
}

func generateIPXEScript(c *echo.Context, machine *infrastructurev1alpha1.TartMachine, host *infrastructurev1alpha1.TartHost) string {
	// TODO: Assets サーバーや Metadata サーバーの URL 組み立てロジックを実装する。
	// 現時点ではプレースホルダーとして簡易的なスクリプトを生成します。
	serverURL := fmt.Sprintf("http://%s", c.Request().Host)

	var sb strings.Builder
	sb.WriteString("#!ipxe\n")

	// カーネルパラメータの組み立て
	params := strings.Join(machine.Spec.KernelParams, " ")
	if machine.Status.BootstrapToken != "" {
		metadataURL := fmt.Sprintf("%s/metadata/%s?token=%s", serverURL, host.Spec.MACAddress, machine.Status.BootstrapToken)
		// Talos Linux を想定したデフォルトのパラメータ追加例
		if params != "" {
			params += " "
		}
		params += fmt.Sprintf("talos.config=%s", metadataURL)
	}

	if params == "" {
		sb.WriteString(fmt.Sprintf("kernel %s\n", machine.Spec.Image))
	} else {
		sb.WriteString(fmt.Sprintf("kernel %s %s\n", machine.Spec.Image, params))
	}
	if machine.Spec.Initrd != "" {
		sb.WriteString(fmt.Sprintf("initrd %s\n", machine.Spec.Initrd))
	}
	sb.WriteString("boot\n")

	return sb.String()
}

func NewServer(cl client.Client, scheme *runtime.Scheme, addr string) *Server {
	return &Server{
		client: cl,
		scheme: scheme,
		addr:   addr,
	}
}

func (s *Server) Addr() string {
	return s.addr
}

func (s *Server) Start(ctx context.Context) error {
	log := crlog.FromContext(ctx).WithName("ipxe")

	server := &http.Server{
		Addr:              s.addr,
		Handler:           NewHandler(s.client, s.scheme),
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
