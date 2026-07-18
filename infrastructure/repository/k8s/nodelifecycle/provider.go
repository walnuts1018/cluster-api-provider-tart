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

package nodelifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	application "github.com/walnuts1018/cluster-api-provider-tart/domain/node/workflow/run_signed_step"
	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
)

var ErrNotFound = errors.New("node lifecycle resource not found")

type Provider struct {
	client client.Client
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func NewProvider(k8sClient client.Client) *Provider {
	return &Provider{client: k8sClient}
}

func (provider *Provider) GetPlan(
	ctx context.Context,
	key client.ObjectKey,
) (application.SignedPlan, error) {
	secret := &corev1.Secret{}
	secretKey := client.ObjectKey{
		Namespace: key.Namespace,
		Name:      key.Name + PlanSecretSuffix,
	}
	if err := provider.client.Get(ctx, secretKey, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return application.SignedPlan{}, ErrNotFound
		}
		return application.SignedPlan{}, fmt.Errorf("get Node Lifecycle Plan Secret: %w", err)
	}
	validated, err := application.ParsePlan(secret.Data[PlanSecretPlanKey])
	if err != nil {
		return application.SignedPlan{}, fmt.Errorf("parse Node Lifecycle Plan: %w", err)
	}
	var signature agentprotocol.Signature
	if err := decodeStrict(secret.Data[PlanSecretSignatureKey], &signature); err != nil {
		return application.SignedPlan{}, fmt.Errorf("parse Node Lifecycle Plan signature: %w", err)
	}
	return application.SignedPlan{
		Plan:      validated.Value(),
		Signature: signature,
	}, nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("body must contain exactly one JSON value")
}
