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

package v1beta1

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func SetupTartClusterWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &infrastructurev1beta1.TartCluster{}).
		WithValidator(&TartClusterCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-tartcluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=tartclusters,verbs=create;update,versions=v1beta1,name=vtartcluster-v1beta1.kb.io,admissionReviewVersions=v1

type TartClusterCustomValidator struct{}

func (v *TartClusterCustomValidator) ValidateCreate(_ context.Context, obj *infrastructurev1beta1.TartCluster) (admission.Warnings, error) {
	return nil, validateTartCluster(obj)
}

func (v *TartClusterCustomValidator) ValidateUpdate(_ context.Context, _, newObj *infrastructurev1beta1.TartCluster) (admission.Warnings, error) {
	return nil, validateTartCluster(newObj)
}

func (v *TartClusterCustomValidator) ValidateDelete(_ context.Context, _ *infrastructurev1beta1.TartCluster) (admission.Warnings, error) {
	return nil, nil
}

func validateTartCluster(cluster *infrastructurev1beta1.TartCluster) error {
	errors := validateArtifactPolicy(cluster.Spec.ArtifactPolicy, field.NewPath("spec", "artifactPolicy"))
	if len(errors) == 0 {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: infrastructurev1beta1.GroupVersion.Group, Kind: "TartCluster"}, cluster.Name, errors)
}
