package main

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	agentclient "github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/client"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/disk"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/inventory"
	agentplan "github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/plan"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

const defaultSystemUUIDPath = "/sys/class/dmi/id/product_uuid"

type config struct {
	controllerURL string
	operationUID  string
	hostUID       string
	systemUUID    string
	bootMAC       string
	tlsCAFile     string
	planKeyID     string
	planKeyFile   string
	preflight     bool
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		slog.Error("Provisioning Agent failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	// TODO: amd64-uefi-ab/v1のpartition/role writer完成後に検証済みPlanをServiceへ渡す。
	// 現段階のbinaryがdisk書き込み成功を誤報しないよう、明示的な診断モードだけを許可する。
	if !cfg.preflight {
		return errors.New("disk execution is not implemented; pass --preflight-only to validate registration, Plan, and disk selection")
	}
	systemUUID := cfg.systemUUID
	if systemUUID == "" {
		value, err := os.ReadFile(defaultSystemUUIDPath)
		if err != nil {
			return fmt.Errorf("read system UUID: %w", err)
		}
		systemUUID = strings.TrimSpace(string(value))
	}
	publicKey, err := loadPlanPublicKey(cfg.planKeyFile)
	if err != nil {
		return err
	}
	httpClient, err := newHTTPClient(cfg.tlsCAFile)
	if err != nil {
		return err
	}
	apiClient, err := agentclient.New(agentclient.Config{
		BaseURL:    cfg.controllerURL,
		HTTPClient: httpClient,
		TrustStore: agentprotocol.StaticTrustStore{cfg.planKeyID: publicKey},
	})
	if err != nil {
		return err
	}
	devices, err := inventory.NewLinuxCollector(inventory.DefaultLinuxPaths()).Collect()
	if err != nil {
		return fmt.Errorf("collect Linux disk inventory: %w", err)
	}
	registration, err := apiClient.Register(ctx, agentprotocol.RegisterRequest{
		APIVersion:      agentprotocol.APIVersion,
		OperationUID:    cfg.operationUID,
		HostUID:         cfg.hostUID,
		AgentInstanceID: uuid.NewString(),
		Inventory:       inventory.ToProtocol(systemUUID, cfg.bootMAC, devices),
	})
	if err != nil {
		return err
	}
	validatedPlan, err := apiClient.FetchPlan(
		ctx,
		cfg.operationUID,
		registration.SessionToken,
		registration.PlanDigest,
	)
	if err != nil {
		return err
	}
	if !time.Now().Before(validatedPlan.Value().Deadline) {
		return errors.New("Plan deadline has expired")
	}
	if err := agentplan.ValidateTargets(validatedPlan); err != nil {
		return fmt.Errorf("validate Plan targets: %w", err)
	}
	target, err := disk.Select(validatedPlan.Value().RootDevice, devices)
	if err != nil {
		return fmt.Errorf("select root disk: %w", err)
	}
	slog.Info(
		"Provisioning Agent preflight completed",
		"operation_uid", cfg.operationUID,
		"target_device", target.Path,
		"disk_count", len(devices),
	)
	return nil
}

func parseConfig(args []string) (config, error) {
	var cfg config
	flags := flag.NewFlagSet("provisioning-agent", flag.ContinueOnError)
	flags.StringVar(&cfg.controllerURL, "controller-url", "", "Agent API HTTPS base URL.")
	flags.StringVar(&cfg.operationUID, "operation-uid", "", "Operation UID assigned by the controller.")
	flags.StringVar(&cfg.hostUID, "host-uid", "", "TartHost UID assigned by the controller.")
	flags.StringVar(&cfg.systemUUID, "system-uuid", "", "Host system UUID. Defaults to Linux DMI product_uuid.")
	flags.StringVar(&cfg.bootMAC, "boot-mac-address", "", "MAC address used to boot the Agent.")
	flags.StringVar(&cfg.tlsCAFile, "tls-ca-file", "", "PEM CA bundle used to verify the Agent API.")
	flags.StringVar(&cfg.planKeyID, "plan-key-id", "", "Trusted Plan signing key ID.")
	flags.StringVar(&cfg.planKeyFile, "plan-key-file", "", "PEM Ed25519 public key used to verify Plans.")
	flags.BoolVar(&cfg.preflight, "preflight-only", false, "Validate registration, signed Plan, and disk selection without writing.")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() != 0 {
		return config{}, errors.New("unexpected positional arguments")
	}
	required := map[string]string{
		"controller-url":   cfg.controllerURL,
		"operation-uid":    cfg.operationUID,
		"host-uid":         cfg.hostUID,
		"boot-mac-address": cfg.bootMAC,
		"plan-key-id":      cfg.planKeyID,
		"plan-key-file":    cfg.planKeyFile,
	}
	for name, value := range required {
		if value == "" {
			return config{}, fmt.Errorf("--%s is required", name)
		}
	}
	return cfg, nil
}

func loadPlanPublicKey(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Plan public key: %w", err)
	}
	block, rest := pem.Decode(data)
	if block == nil || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("Plan public key file must contain exactly one PEM block")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse Plan public key: %w", err)
	}
	publicKey, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("Plan public key must be Ed25519")
	}
	return publicKey, nil
}

func newHTTPClient(caFile string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}
	if caFile != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system CA pool: %w", err)
		}
		data, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read Agent API CA bundle: %w", err)
		}
		if !roots.AppendCertsFromPEM(data) {
			return nil, errors.New("Agent API CA bundle contains no certificates")
		}
		tlsConfig.RootCAs = roots
	}
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Transport: transport}, nil
}
