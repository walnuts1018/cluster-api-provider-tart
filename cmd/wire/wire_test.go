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

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestInitializeReconcilersBuildsV1Beta1Composition(t *testing.T) {
	t.Parallel()

	testScheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(testScheme); err != nil {
		t.Fatalf("failed to add core scheme: %v", err)
	}
	if err := infrastructurev1beta1.AddToScheme(testScheme); err != nil {
		t.Fatalf("failed to add infrastructure scheme: %v", err)
	}

	fakeClient := fake.NewClientBuilder().WithScheme(testScheme).Build()

	reconcilers, err := InitializeReconcilers(fakeClient, testScheme)
	if err != nil {
		t.Fatalf("InitializeReconcilers returned error: %v", err)
	}
	if reconcilers.TartMachineV1Beta1 == nil {
		t.Fatal("TartMachine v1beta1 reconciler is nil")
	}
	if reconcilers.TartHostOperation == nil {
		t.Fatal("TartHostOperation reconciler is nil")
	}
	if reconcilers.Driver == nil {
		t.Fatal("driver service is nil")
	}
}

func TestInitializeReconcilersDoesNotFreezeMutableTartMachineWorkflow(t *testing.T) {
	t.Parallel()

	reconcilers, err := InitializeReconcilers(fake.NewClientBuilder().Build(), runtime.NewScheme())
	if err != nil {
		t.Fatalf("InitializeReconcilers() error = %v", err)
	}
	if reconcilers.TartMachineV1Beta1 == nil {
		t.Fatal("TartMachineV1Beta1 reconciler is nil")
	}
	if reconcilers.TartMachineV1Beta1.Lifecycle != nil {
		t.Fatal("TartMachineV1Beta1 Lifecycle must be composed at reconcile time so startup configuration can replace Provisioner and Cleaner")
	}
}
