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

package agentapi

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	metav1apply "k8s.io/client-go/applyconfigurations/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

const planWriterFieldManager = "tart-initial-provisioning-plan"

// PlanWriter は署名済みPlanをOperation所有のimmutable Secretへ保存する。
type PlanWriter struct {
	client client.Client
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;create;patch

func NewPlanWriter(k8sClient client.Client) *PlanWriter {
	return &PlanWriter{client: k8sClient}
}

func (writer *PlanWriter) Write(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	plan agentprotocol.ValidatedPlan,
	signature agentprotocol.Signature,
) error {
	if operation == nil || operation.Name == "" || operation.UID == "" {
		return fmt.Errorf("persist Agent Plan: TartHostOperation name and UID are required")
	}
	if plan.Value().OperationUID != operation.Spec.OperationID {
		return fmt.Errorf("persist Agent Plan: operation ID does not match")
	}
	planDigest, err := plan.Digest()
	if err != nil {
		return fmt.Errorf("persist Agent Plan: calculate digest: %w", err)
	}
	if planDigest.String() != operation.Spec.PlanDigest {
		return fmt.Errorf("persist Agent Plan: digest does not match TartHostOperation")
	}
	planJSON, err := plan.CanonicalJSON()
	if err != nil {
		return fmt.Errorf("persist Agent Plan: canonicalize Plan: %w", err)
	}
	signatureJSON, err := json.Marshal(signature)
	if err != nil {
		return fmt.Errorf("persist Agent Plan: encode signature: %w", err)
	}
	data := map[string][]byte{
		PlanSecretPlanKey:      planJSON,
		PlanSecretSignatureKey: signatureJSON,
	}
	key := client.ObjectKey{
		Namespace: operation.Namespace,
		Name:      operation.Name + PlanSecretSuffix,
	}

	current := &corev1.Secret{}
	if err := writer.client.Get(ctx, key, current); err == nil {
		return validateExistingPlanSecret(current, operation, data)
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get Agent Plan Secret: %w", err)
	}

	applyConfig := corev1apply.Secret(key.Name, key.Namespace).
		WithImmutable(true).
		WithType(corev1.SecretTypeOpaque).
		WithData(data).
		WithOwnerReferences(
			metav1apply.OwnerReference().
				WithAPIVersion(infrastructurev1beta1.GroupVersion.String()).
				WithKind("TartHostOperation").
				WithName(operation.Name).
				WithUID(operation.UID).
				WithController(true).
				WithBlockOwnerDeletion(true),
		)
	if err := writer.client.Apply(
		ctx,
		applyConfig,
		client.FieldOwner(planWriterFieldManager),
		client.ForceOwnership,
	); err != nil {
		if apierrors.IsAlreadyExists(err) || apierrors.IsConflict(err) {
			if getErr := writer.client.Get(ctx, key, current); getErr != nil {
				return fmt.Errorf("get competing Agent Plan Secret: %w", getErr)
			}
			return validateExistingPlanSecret(current, operation, data)
		}
		return fmt.Errorf("apply Agent Plan Secret: %w", err)
	}
	return nil
}

func validateExistingPlanSecret(
	secret *corev1.Secret,
	operation *infrastructurev1beta1.TartHostOperation,
	expectedData map[string][]byte,
) error {
	if secret.Immutable == nil || !*secret.Immutable {
		return fmt.Errorf("existing Agent Plan Secret is mutable")
	}
	if !apiequality.Semantic.DeepEqual(secret.Data, expectedData) {
		return fmt.Errorf("existing Agent Plan Secret content conflicts with TartHostOperation")
	}
	for _, owner := range secret.OwnerReferences {
		if owner.APIVersion == infrastructurev1beta1.GroupVersion.String() &&
			owner.Kind == "TartHostOperation" &&
			owner.Name == operation.Name &&
			owner.UID == operation.UID {
			return nil
		}
	}
	return fmt.Errorf("existing Agent Plan Secret is not owned by TartHostOperation")
}
