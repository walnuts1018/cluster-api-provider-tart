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

package drivertarget

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func TestServiceBuildReturnsBootMACOnlyWhenRedfishIsNotConfigured(t *testing.T) {
	t.Parallel()

	service := NewService(newFakeClient(t))
	target, err := service.Build(t.Context(), testHost(nil))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if got := target.BootMACAddress().String(); got != "02:00:00:00:00:01" {
		t.Fatalf("BootMACAddress() = %q", got)
	}
	if _, ok := target.RedfishAccess(); ok {
		t.Fatal("RedfishAccess() ok = true, want false")
	}
}

func TestServiceBuildLoadsRedfishAccessFromSecrets(t *testing.T) {
	t.Parallel()

	host := testHost(&infrastructurev1beta1.RedfishManagement{
		Endpoint:          "https://bmc.example.test",
		CABundleSecretRef: &corev1.LocalObjectReference{Name: "bmc-ca"},
		SPKIPins:          []string{"sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
	})
	service := NewService(newFakeClient(t,
		credentialsSecret(),
		caBundleSecret(),
	))
	target, err := service.Build(t.Context(), host)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	access, ok := target.RedfishAccess()
	if !ok {
		t.Fatal("RedfishAccess() ok = false, want true")
	}
	if access.Endpoint() != "https://bmc.example.test" {
		t.Fatalf("Endpoint() = %q", access.Endpoint())
	}
	if access.Username() != "admin" || access.Password() != "secret" {
		t.Fatalf("credentials = %q/%q", access.Username(), access.Password())
	}
	if string(access.CABundlePEM()) != "PEM" {
		t.Fatalf("CABundlePEM() = %q", access.CABundlePEM())
	}
}

func TestServiceBuildRejectsMissingCredentialKey(t *testing.T) {
	t.Parallel()

	host := testHost(&infrastructurev1beta1.RedfishManagement{
		Endpoint: "https://bmc.example.test",
	})
	secret := credentialsSecret()
	delete(secret.Data, credentialsPasswordKey)
	service := NewService(newFakeClient(t, secret))
	if _, err := service.Build(t.Context(), host); err == nil {
		t.Fatal("Build() error = nil, want missing password error")
	}
}

func testHost(redfish *infrastructurev1beta1.RedfishManagement) *infrastructurev1beta1.TartHost {
	return &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-a",
			Namespace: "default",
		},
		Spec: infrastructurev1beta1.TartHostSpec{
			Identifiers: infrastructurev1beta1.HostIdentifiers{
				BootMACAddress: "02:00:00:00:00:01",
			},
			Management: infrastructurev1beta1.HostManagement{
				PowerDriver:          "redfish",
				BootDriver:           "redfish",
				CredentialsSecretRef: &corev1.LocalObjectReference{Name: "bmc-credentials"},
				Redfish:              redfish,
			},
		},
	}
}

func credentialsSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bmc-credentials",
			Namespace: "default",
		},
		Data: map[string][]byte{
			credentialsUsernameKey: []byte("admin"),
			credentialsPasswordKey: []byte("secret"),
		},
	}
}

func caBundleSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bmc-ca",
			Namespace: "default",
		},
		Data: map[string][]byte{
			caBundleKey: []byte("PEM"),
		},
	}
}

func newFakeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
}
