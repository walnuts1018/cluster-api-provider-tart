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
	"io/fs"
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
	var credentialsDir string
	flag.StringVar(&tlsDir, "tls-dir", "", "Directory for a generated Agent API TLS certificate and key.")
	flag.StringVar(&credentialsDir, "credentials-dir", "", "Directory for the Agent Plan signing key.")
	flag.Parse()
	if err := run(context.Background(), tlsDir, credentialsDir); err != nil {
		slog.Error("Failed to initialize provisioning credentials", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, tlsDir, credentialsDir string) error {
	if tlsDir != "" || credentialsDir != "" {
		if tlsDir != "" {
			if err := initializeTLS(tlsDir, os.Getenv("TART_PROVISIONING_ADDRESS")); err != nil {
				return err
			}
		}
		if credentialsDir != "" {
			if err := initializeCredentials(ctx, credentialsDir); err != nil {
				return err
			}
		}
		return nil
	}

	return initializeCredentialsInCluster(ctx)
}

func initializeCredentialsInCluster(ctx context.Context) error {
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
	return initializeCredentialsWithClient(ctx, clientset, namespace, "")
}

func initializeCredentials(ctx context.Context, directory string) error {
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
	return initializeCredentialsWithClient(ctx, clientset, namespace, directory)
}

func initializeCredentialsWithClient(ctx context.Context, clientset kubernetes.Interface, namespace, directory string) error {
	secrets := clientset.CoreV1().Secrets(namespace)
	var secret *corev1.Secret
	if existing, err := secrets.Get(ctx, credentialSecretName, metav1.GetOptions{}); err == nil {
		secret = existing
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get provisioning credential Secret: %w", err)
	} else {
		_, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return fmt.Errorf("generate Agent Plan signing key: %w", err)
		}
		encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
		if err != nil {
			return fmt.Errorf("encode Agent Plan signing key: %w", err)
		}
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: credentialSecretName, Namespace: namespace},
			Type:       corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				planPrivateKeyName: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}),
			},
		}
		if created, err := secrets.Create(ctx, secret, metav1.CreateOptions{}); err == nil {
			secret = created
		} else if apierrors.IsAlreadyExists(err) {
			secret, err = secrets.Get(ctx, credentialSecretName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("get concurrently created provisioning credential Secret: %w", err)
			}
		} else {
			return fmt.Errorf("create provisioning credential Secret: %w", err)
		}
	}
	key, ok := secret.Data[planPrivateKeyName]
	if !ok || len(key) == 0 {
		return fmt.Errorf("provisioning credential Secret must contain %q", planPrivateKeyName)
	}
	if directory == "" {
		return nil
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create provisioning credential directory: %w", err)
	}
	// Secret volumeをPodのadmission時点で要求すると、Secret生成前のmount競合が起きるため、
	// init containerで取得した鍵だけをメモリ上の一時領域へ展開してからmanagerを起動する。
	path := filepath.Join(directory, planPrivateKeyName)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove existing provisioning credential: %w", err)
	}
	if err := os.WriteFile(path, key, 0o400); err != nil {
		return fmt.Errorf("write provisioning credential: %w", err)
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
	crtPath := filepath.Join(directory, "agent-api.crt")
	if err := os.Remove(crtPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove existing Agent API certificate: %w", err)
		}
	}
	if err := os.WriteFile(crtPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}), 0o444); err != nil {
		return fmt.Errorf("write Agent API certificate: %w", err)
	}
	keyPath := filepath.Join(directory, "agent-api.key")
	if err := os.Remove(keyPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove existing Agent API TLS key: %w", err)
		}
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0o400); err != nil {
		return fmt.Errorf("write Agent API TLS key: %w", err)
	}
	return nil
}
