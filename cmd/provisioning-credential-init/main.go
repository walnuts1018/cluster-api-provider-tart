package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	credentialSecretName = "tart-provisioning-credentials"
	planPrivateKeyName   = "agent-plan-private.pem"
)

func main() {
	var tlsDir string
	flag.StringVar(&tlsDir, "tls-dir", "", "Directory for a generated Agent API TLS certificate and key.")
	flag.Parse()
	if err := run(context.Background(), tlsDir); err != nil {
		slog.Error("Failed to initialize provisioning credentials", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, tlsDir string) error {
	if tlsDir != "" {
		return initializeTLS(tlsDir, os.Getenv("TART_PROVISIONING_ADDRESS"))
	}
	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		return errors.New("POD_NAMESPACE is required")
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("load Kubernetes configuration: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	secrets := clientset.CoreV1().Secrets(namespace)
	if _, err := secrets.Get(ctx, credentialSecretName, metav1.GetOptions{}); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get provisioning credential Secret: %w", err)
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate Agent Plan signing key: %w", err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("encode Agent Plan signing key: %w", err)
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: credentialSecretName, Namespace: namespace},
		Type:       corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			planPrivateKeyName: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}),
		},
	}
	if _, err := secrets.Create(ctx, secret, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create provisioning credential Secret: %w", err)
	}
	return nil
}

func initializeTLS(directory, address string) error {
	if address == "" {
		return errors.New("TART_PROVISIONING_ADDRESS is required")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate Agent API TLS key: %w", err)
	}
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generate Agent API certificate serial number: %w", err)
	}
	now := time.Now()
	template := x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{CommonName: "tart-provisioning-agent"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	if ip := net.ParseIP(address); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{address}
	}
	certificate, err := x509.CreateCertificate(rand.Reader, &template, &template, publicKey, privateKey)
	if err != nil {
		return fmt.Errorf("create Agent API certificate: %w", err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("encode Agent API TLS key: %w", err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create Agent API TLS directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "agent-api.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}), 0o444); err != nil {
		return fmt.Errorf("write Agent API certificate: %w", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "agent-api.key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0o400); err != nil {
		return fmt.Errorf("write Agent API TLS key: %w", err)
	}
	return nil
}
