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

// DialMaintenanceは未構成Talos nodeのmaintenance APIへ接続する。connectionはTLSで暗号化されるが、server certificateが自己署名で相互identity検証もないため認証されない。呼び出し側は応答を信頼する前にendpointをexpected Host identity(MAC/DHCP、boot attempt、observed system UUID)へbindし、そのbindなしにこのconnectionでconfiguration apply requestを送信しない。詳細は.agents/skills/talos/SKILL.md#maintenance-apiを参照する。
func DialMaintenance(ctx context.Context, endpoint string) (*Client, error) {
	if err := validateEndpoint(endpoint); err != nil {
		return nil, err
	}

	return dial(ctx, endpoint, &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // maintenance mode Talos endpoints present a self-signed certificate by design.
	})
}

// DialAuthenticatedはclusterのTalos PKIから発行されたclient certificateを使ってTalos nodeへ接続する。clientCertPEM、clientKeyPEM、caPEMはPEM encodedで、Clusterのimmutableなgeneration-scoped secret bundleから取得する。呼び出し側はそれらをSecretの外へlogまたは永続化しない。
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

// DialAuthenticatedFromConfigurationはimmutableなcomplete machine configurationから短命なadmin client certificateを導出してauthenticated Talos APIへ接続する。configurationと生成したcredentialはdial中だけメモリに保持する。
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
