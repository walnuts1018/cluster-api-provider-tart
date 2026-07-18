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

package operation

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

type ReferenceReader struct {
	client client.Client
}

func NewReferenceReader(k8sClient client.Client) *ReferenceReader {
	return &ReferenceReader{client: k8sClient}
}

func (reader *ReferenceReader) GetHost(
	ctx context.Context,
	ref infrastructurev1beta1.ResourceReference,
) (*infrastructurev1beta1.TartHost, error) {
	host := &infrastructurev1beta1.TartHost{}
	if err := reader.client.Get(ctx, client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}, host); err != nil {
		return nil, fmt.Errorf("get TartHost for Operation: %w", err)
	}
	if host.UID != ref.UID {
		return nil, fmt.Errorf("TartHost UID mismatch: expected %s, got %s", ref.UID, host.UID)
	}
	return host, nil
}

func (reader *ReferenceReader) GetMachine(
	ctx context.Context,
	ref infrastructurev1beta1.ResourceReference,
) (*infrastructurev1beta1.TartMachine, error) {
	machine := &infrastructurev1beta1.TartMachine{}
	if err := reader.client.Get(ctx, client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}, machine); err != nil {
		return nil, fmt.Errorf("get TartMachine for Cleaning policy: %w", err)
	}
	if machine.UID != ref.UID {
		return nil, fmt.Errorf("TartMachine UID mismatch: expected %s, got %s", ref.UID, machine.UID)
	}
	return machine, nil
}
