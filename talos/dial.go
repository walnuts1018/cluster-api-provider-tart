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

package talos

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
)

// ErrEndpointEmptyはTalos接続先が空でdialできないことを示す。
var ErrEndpointEmpty = errors.New("talos endpoint is empty")

// DialMaintenance connects to an unconfigured Talos node's maintenance API. The
// connection is TLS-encrypted but not authenticated: the server certificate is
// self-signed and there is no mutual identity verification. Callers must bind the
// endpoint to an expected Host identity (MAC/DHCP, boot attempt, observed system UUID)
// before trusting any response, and must not send configuration apply requests over
// this connection without that binding. See .agents/skills/talos/SKILL.md#maintenance-api.
func DialMaintenance(ctx context.Context, endpoint string) (*Client, error) {
	if err := validateEndpoint(endpoint); err != nil {
		return nil, err
	}

	return dial(ctx, endpoint, &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // maintenance mode Talos endpoints present a self-signed certificate by design.
	})
}

// DialAuthenticated connects to a Talos node using a client certificate issued from the
// cluster's Talos PKI. clientCertPEM/clientKeyPEM/caPEM are PEM-encoded and are expected
// to come from the Cluster's immutable, generation-scoped secret bundle; callers must
// never log or persist them outside that Secret.
func DialAuthenticated(ctx context.Context, endpoint string, clientCertPEM, clientKeyPEM, caPEM []byte) (*Client, error) {
	if err := validateEndpoint(endpoint); err != nil {
		return nil, err
	}

	cert, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse talos client certificate: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse talos CA certificate: no certificates found")
	}
	return dial(ctx, endpoint, &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
	})
}

func validateEndpoint(endpoint string) error {
	if strings.TrimSpace(endpoint) == "" {
		return ErrEndpointEmpty
	}

	return nil
}
