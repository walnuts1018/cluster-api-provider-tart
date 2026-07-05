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

package agentapi

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	crlog "sigs.k8s.io/controller-runtime/pkg/log"
)

type Server struct {
	address  string
	certFile string
	keyFile  string
	handler  http.Handler
}

func NewServer(address, certFile, keyFile string, handler http.Handler) *Server {
	return &Server{
		address:  address,
		certFile: certFile,
		keyFile:  keyFile,
		handler:  handler,
	}
}

func (server *Server) Start(ctx context.Context) error {
	certificate, err := tls.LoadX509KeyPair(server.certFile, server.keyFile)
	if err != nil {
		return fmt.Errorf("load Agent API TLS certificate: %w", err)
	}
	listener, err := net.Listen("tcp", server.address)
	if err != nil {
		return fmt.Errorf("listen for Agent API: %w", err)
	}
	tlsListener := tls.NewListener(listener, &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
	})
	httpServer := &http.Server{
		Handler:           server.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if shutdownErr := httpServer.Shutdown(shutdownCtx); shutdownErr != nil {
			crlog.FromContext(ctx).Error(shutdownErr, "Failed to shut down Agent API server")
		}
	}()

	if err := httpServer.Serve(tlsListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve Agent API: %w", err)
	}
	return nil
}

func (*Server) NeedLeaderElection() bool {
	return true
}
