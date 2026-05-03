package ipxe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
)

type Server struct {
	client client.Client
	addr   string
}

func NewHandler(cl client.Client) http.Handler {
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
		normalizedMAC, err := NormalizeMAC(mac)
		if err != nil {
			return c.String(http.StatusBadRequest, fmt.Sprintf("invalid mac address format: %s", mac))
		}

		ctx := c.Request().Context()

		// Use index to find TartHost by MAC address
		var hosts infrastructurev1alpha1.TartHostList
		if err := cl.List(ctx, &hosts, client.MatchingFields{"spec.macAddress": normalizedMAC}); err != nil {
			return c.String(http.StatusInternalServerError, "failed to list hosts by macAddress")
		}

		// Also check bootMACAddress if not found by macAddress
		var bootHosts infrastructurev1alpha1.TartHostList
		if err := cl.List(ctx, &bootHosts, client.MatchingFields{"spec.bootMACAddress": normalizedMAC}); err != nil {
			return c.String(http.StatusInternalServerError, "failed to list hosts by bootMACAddress")
		}

		var targetHost *infrastructurev1alpha1.TartHost
		if len(bootHosts.Items) > 0 {
			targetHost = &bootHosts.Items[0]
		} else if len(hosts.Items) > 0 {
			targetHost = &hosts.Items[0]
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

		script := generateIPXEScript(c, &machine, targetHost, normalizedMAC)

		return c.Blob(http.StatusOK, "text/plain; charset=utf-8", []byte(script))
	})
	return e
}

func NormalizeMAC(mac string) (string, error) {
	hw, err := net.ParseMAC(mac)
	if err != nil {
		return "", err
	}
	return hw.String(), nil
}

func generateIPXEScript(_ *echo.Context, machine *infrastructurev1alpha1.TartMachine, _ *infrastructurev1alpha1.TartHost, requestedMAC string) string {
	// TODO: Assets サーバーや Metadata サーバーの URL 組み立てロジックを実装する。
	// 現時点ではプレースホルダーとして簡易的なスクリプトを生成します。
	// serverURL := fmt.Sprintf("http://%s", c.Request().Host)

	var sb strings.Builder
	sb.WriteString("#!ipxe\n")

	// カーネルパラメータの組み立て
	params := strings.Join(machine.Spec.KernelParams, " ")

	// TODO: metadata URL endpoint is not implemented yet. Feature-gate or implement it before enabling talos.config.
	// if machine.Status.BootstrapToken != "" {
	// 	metadataURL := fmt.Sprintf("%s/metadata/%s?token=%s", serverURL, requestedMAC, machine.Status.BootstrapToken)
	// 	if params != "" {
	// 		params += " "
	// 	}
	// 	params += fmt.Sprintf("talos.config=%s", metadataURL)
	// }

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

func NewServer(cl client.Client, addr string) *Server {
	return &Server{
		client: cl,
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
		Handler:           NewHandler(s.client),
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
