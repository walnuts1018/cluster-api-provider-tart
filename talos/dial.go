package talos

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/role"
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

// DialAuthenticatedFromConfiguration derives a short-lived admin client
// certificate from the immutable complete machine configuration and connects to
// the authenticated Talos API. The configuration and generated credentials stay
// in memory for the duration of the dial.
func DialAuthenticatedFromConfiguration(ctx context.Context, endpoint string, configuration []byte) (*Client, error) {
	if err := validateEndpoint(endpoint); err != nil {
		return nil, err
	}
	if len(configuration) == 0 {
		return nil, errors.New("talos machine configuration is empty")
	}

	config, err := configloader.NewFromBytes(configuration)
	if err != nil {
		return nil, fmt.Errorf("load talos machine configuration: %w", err)
	}
	bundle, err := secrets.NewBundleFromConfig(secrets.NewFixedClock(time.Now()), config)
	if err != nil {
		return nil, fmt.Errorf("derive talos credentials from machine configuration: %w", err)
	}
	if bundle.Certs == nil || bundle.Certs.OS == nil {
		return nil, errors.New("talos machine configuration has no OS certificate authority")
	}
	certificate, err := bundle.GenerateTalosAPIClientCertificate(role.MakeSet(role.Admin))
	if err != nil {
		return nil, fmt.Errorf("generate talos API client certificate: %w", err)
	}
	return DialAuthenticated(ctx, endpoint, certificate.Crt, certificate.Key, bundle.Certs.OS.Crt)
}

func validateEndpoint(endpoint string) error {
	if strings.TrimSpace(endpoint) == "" {
		return ErrEndpointEmpty
	}

	return nil
}
