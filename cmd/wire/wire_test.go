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

package wire

import (
	"testing"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestInitializeReconcilersSharesHostService(t *testing.T) {
	t.Parallel()

	testScheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(testScheme); err != nil {
		t.Fatalf("failed to add core scheme: %v", err)
	}
	if err := infrastructurev1alpha1.AddToScheme(testScheme); err != nil {
		t.Fatalf("failed to add infrastructure scheme: %v", err)
	}

	fakeClient := fake.NewClientBuilder().WithScheme(testScheme).Build()

	reconcilers, err := InitializeReconcilers(fakeClient, testScheme)
	if err != nil {
		t.Fatalf("InitializeReconcilers returned error: %v", err)
	}
	if reconcilers.TartHost == nil {
		t.Fatal("TartHost reconciler is nil")
	}
	if reconcilers.TartMachine == nil {
		t.Fatal("TartMachine reconciler is nil")
	}
	if reconcilers.TartHost.HostService == nil {
		t.Fatal("TartHost host service is nil")
	}
	if reconcilers.TartMachine.HostService == nil {
		t.Fatal("TartMachine host service is nil")
	}
	if reconcilers.TartMachine.Provisioning == nil {
		t.Fatal("TartMachine provisioning service is nil")
	}
	if reconcilers.TartHost.HostService != reconcilers.TartMachine.HostService {
		t.Fatal("expected reconcilers to share a single host service instance")
	}
}
