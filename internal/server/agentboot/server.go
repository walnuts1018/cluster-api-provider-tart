// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package agentboot

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"time"

	crlog "sigs.k8s.io/controller-runtime/pkg/log"
)

type Server struct {
	address  string
	certFile string
	keyFile  string
	handler  *Handler
}

func NewServer(address, certFile, keyFile string, handler *Handler) *Server {
	return &Server{address: address, certFile: certFile, keyFile: keyFile, handler: handler}
}

func (server *Server) Start(ctx context.Context) error {
	defer func() {
		if err := server.handler.artifact.Close(); err != nil {
			crlog.FromContext(ctx).Error(err, "Failed to close Agent Artifact")
		}
	}()
	certificate, err := tls.LoadX509KeyPair(server.certFile, server.keyFile)
	if err != nil {
		return fmt.Errorf("load Agent boot TLS certificate: %w", err)
	}
	listener := &http.Server{
		Addr:              server.address,
		Handler:           server.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{certificate},
		},
	}
	errCh := make(chan error, 1)
	go func() {
		crlog.FromContext(ctx).Info("Starting Agent boot HTTPS server", "addr", server.address)
		errCh <- listener.ListenAndServeTLS("", "")
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := listener.Shutdown(shutdownCtx); err != nil {
			crlog.FromContext(ctx).Error(err, "Failed to shut down Agent boot HTTPS server")
		}
		if serveErr := <-errCh; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("serve Agent boot HTTPS: %w", serveErr)
		}
		return nil
	case serveErr := <-errCh:
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("serve Agent boot HTTPS: %w", serveErr)
		}
		return nil
	}
}

func (server *Server) NeedLeaderElection() bool {
	return true
}
