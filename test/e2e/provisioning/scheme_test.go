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

package provisioning

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func TestNewSchemeRegistersBootstrapDependencies(t *testing.T) {
	t.Parallel()

	scheme := newScheme()

	if _, err := scheme.New(appsv1.SchemeGroupVersion.WithKind("Deployment")); err != nil {
		t.Fatalf("expected apps/v1 Deployment to be registered: %v", err)
	}

	if _, err := scheme.New(infrastructurev1beta1.GroupVersion.WithKind("TartHost")); err != nil {
		t.Fatalf("expected infrastructure API to be registered: %v", err)
	}

	if _, err := scheme.New(clusterv1.GroupVersion.WithKind("Cluster")); err != nil {
		t.Fatalf("expected Cluster API core API to be registered: %v", err)
	}
}
