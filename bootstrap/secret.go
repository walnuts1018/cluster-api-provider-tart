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

package bootstrap

import (
	"bytes"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
)

const (
	BootstrapSecretType = corev1.SecretType("cluster.x-k8s.io/secret")
	BootstrapSecretKey  = "value"
	ClusterNameLabel    = "cluster.x-k8s.io/cluster-name"
)

var (
	ErrMissingConfigSecret        = errors.New("bootstrap configuration Secret is missing")
	ErrConfigSecretNotImmutable   = errors.New("bootstrap configuration Secret must be immutable")
	ErrConfigSecretEmpty          = errors.New("bootstrap configuration Secret has no data")
	ErrInvalidSecretMetadata      = errors.New("bootstrap Secret metadata is incomplete")
	ErrOwnerReferenceIncomplete   = errors.New("bootstrap Secret owner reference is incomplete")
	ErrOwnerReferenceInvalid      = errors.New("bootstrap Secret owner reference is invalid")
	ErrCompleteConfigurationEmpty = errors.New("complete machine configuration is empty")
)

// ValidateConfigSecretはユーザー所有のraw configuration inputがimmutableであり、空でないことを確認する。
// Secretの値は返さず、controllerがStatusやlogへ誤って出力しない境界にする。
func ValidateConfigSecret(secret *corev1.Secret) error {
	if secret == nil {
		return ErrMissingConfigSecret
	}
	if secret.Immutable == nil || !*secret.Immutable {
		return fmt.Errorf("%w: %s", ErrConfigSecretNotImmutable, secret.Name)
	}
	for key, value := range secret.Data {
		if key != "" && len(value) > 0 {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrConfigSecretEmpty, secret.Name)
}

// BuildSecretは完全にrender済みのTalos machine configurationからCAPI Bootstrap Secretを作成する。
// raw patchをこの関数へ渡すことは呼び出し側の責務違反であり、completeConfigurationは非空だけを受け付ける。
func BuildSecret(namespace, name, clusterName string, owner metav1.OwnerReference, completeConfiguration []byte) (*corev1.Secret, error) {
	if namespace == "" || name == "" || clusterName == "" {
		return nil, ErrInvalidSecretMetadata
	}
	if owner.APIVersion == "" || owner.Kind == "" || owner.Name == "" || owner.UID == "" {
		return nil, ErrOwnerReferenceIncomplete
	}
	if owner.APIVersion != bootstrapv1alpha1.GroupVersion.String() || owner.Kind != "TartBootstrapConfig" {
		return nil, ErrOwnerReferenceInvalid
	}
	if len(bytes.TrimSpace(completeConfiguration)) == 0 {
		return nil, ErrCompleteConfigurationEmpty
	}

	controller := true
	owner.Controller = &controller
	return &corev1.Secret{
		Name:            name,
		Namespace:       namespace,
		Labels:          map[string]string{ClusterNameLabel: clusterName},
		OwnerReferences: []metav1.OwnerReference{owner},
		Type:            BootstrapSecretType,
		Immutable:       new(true),
		Data: map[string][]byte{
			BootstrapSecretKey: bytes.Clone(completeConfiguration),
		},
	}, nil
}

// IsContractSecretは既存SecretがBootstrap contractを満たすかを判定する。
// Secretのdataは比較せず、存在とcontractだけを確認する。
func IsContractSecret(secret *corev1.Secret, clusterName string, ownerUID types.UID) bool {
	if secret == nil || secret.Type != BootstrapSecretType || secret.Immutable == nil || !*secret.Immutable || clusterName == "" {
		return false
	}
	if len(secret.Data) != 1 || len(secret.Data[BootstrapSecretKey]) == 0 {
		return false
	}
	if secret.Labels[ClusterNameLabel] != clusterName {
		return false
	}
	if len(secret.OwnerReferences) != 1 || secret.OwnerReferences[0].APIVersion != bootstrapv1alpha1.GroupVersion.String() || secret.OwnerReferences[0].Kind != "TartBootstrapConfig" || secret.OwnerReferences[0].Name == "" || secret.OwnerReferences[0].UID == "" || secret.OwnerReferences[0].Controller == nil || !*secret.OwnerReferences[0].Controller {
		return false
	}
	return ownerUID == "" || secret.OwnerReferences[0].UID == ownerUID
}
