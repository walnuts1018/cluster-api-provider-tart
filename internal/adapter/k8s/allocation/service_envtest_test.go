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

package allocation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/test/envtestutil"
)

const projectRoot = "../../../.."

func TestServiceReserveAllowsOneOfOneHundredConcurrentMachinesWithAPIServer(t *testing.T) {
	crd := loadTartHostCRDWithV1Beta1Storage(t)
	testEnvironment := &envtest.Environment{
		CRDs:                  []*apiextensionsv1.CustomResourceDefinition{crd},
		BinaryAssetsDirectory: envtestutil.RequireBinaryAssetsDirectory(t, projectRoot),
	}
	cfg, err := testEnvironment.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		stopped := make(chan error, 1)
		go func() {
			stopped <- testEnvironment.Stop()
		}()
		select {
		case err := <-stopped:
			if err != nil {
				t.Errorf("stop envtest: %v", err)
			}
		case <-stopCtx.Done():
			t.Errorf("stop envtest: %v", stopCtx.Err())
		}
	})

	testScheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(testScheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	k8sClient, err := client.New(cfg, client.Options{Scheme: testScheme})
	if err != nil {
		t.Fatalf("create Kubernetes client: %v", err)
	}
	ctx := t.Context()
	host := matchingHost()
	host.ResourceVersion = ""
	desiredStatus := host.Status
	if err := k8sClient.Create(ctx, host); err != nil {
		t.Fatalf("create TartHost: %v", err)
	}
	host.Status = desiredStatus
	if err := k8sClient.Status().Update(ctx, host); err != nil {
		t.Fatalf("update TartHost status: %v", err)
	}

	service := NewService(k8sClient)
	requirements := matchingRequirements(t)
	const goroutines = 100
	var successes atomic.Int32
	var unexpectedErrors atomic.Int32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func() {
			defer wg.Done()
			machine := concurrentMachine(i)
			_, err := service.Reserve(ctx, machine, requirements)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrNoMatchingHost):
			default:
				unexpectedErrors.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := successes.Load(); got != 1 {
		t.Fatalf("successful reservations = %d, want 1", got)
	}
	if got := unexpectedErrors.Load(); got != 0 {
		t.Fatalf("unexpected errors = %d, want 0", got)
	}
}

func loadTartHostCRDWithV1Beta1Storage(t *testing.T) *apiextensionsv1.CustomResourceDefinition {
	t.Helper()
	path := filepath.Join(
		projectRoot, "config", "crd", "bases",
		"infrastructure.cluster.x-k8s.io_tarthosts.yaml",
	)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read TartHost CRD: %v", err)
	}
	jsonData, err := yaml.ToJSON(data)
	if err != nil {
		t.Fatalf("convert TartHost CRD to JSON: %v", err)
	}
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := json.Unmarshal(jsonData, crd); err != nil {
		t.Fatalf("decode TartHost CRD: %v", err)
	}
	for i := range crd.Spec.Versions {
		crd.Spec.Versions[i].Storage = crd.Spec.Versions[i].Name == infrastructurev1beta1.GroupVersion.Version
	}
	return crd
}
