package ipxe

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
)

const dummyScript = `#!ipxe
echo Tart placeholder boot script
sleep 3
`

type Server struct {
	addr string
}

func NewHandler() http.Handler {
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
		return c.Blob(http.StatusOK, "text/plain; charset=utf-8", []byte(dummyScript))
	})

	return e
}

func NewServer(addr string) *Server {
	return &Server{addr: addr}
}

func (s *Server) Addr() string {
	return s.addr
}

func (s *Server) Start(ctx context.Context) error {
	log := crlog.FromContext(ctx).WithName("ipxe")

	server := &http.Server{
		Addr:              s.addr,
		Handler:           NewHandler(),
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
