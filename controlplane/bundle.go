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

package controlplane

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"uuid"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	ClusterNameLabel   = "cluster.x-k8s.io/cluster-name"
	ClusterIDLabel     = "tart.cluster.x-k8s.io/cluster-id"
	GenerationLabel    = "tart.cluster.x-k8s.io/secret-generation"
	BundleStateLabel   = "tart.cluster.x-k8s.io/bundle-state"
	BundleStatePending = "Pending"
	BundleStateActive  = "Active"
)

var (
	ErrInvalidClusterIdentity  = errors.New("invalid cluster identity")
	ErrInvalidBundleGeneration = errors.New("invalid bundle generation")
	ErrBundleDataIncomplete    = errors.New("bundle data is incomplete")
	ErrRotationTargetMismatch  = errors.New("rotation target mismatch")
	ErrBundleOwnerIncomplete   = errors.New("bundle owner reference is incomplete")
)

// NextGenerationはactive generationから単調増加する次世代番号を返す。
func NextGeneration(current int32) (int32, error) {
	if current < 0 || current == int32(^uint32(0)>>1) {
		return 0, ErrInvalidBundleGeneration
	}
	if current == 0 {
		return 1, nil
	}
	return current + 1, nil
}

// BundleNameはCluster IDとgenerationから世代単位のimmutable Secret名を決定する。
func BundleName(clusterName, clusterID string, generation int32) (string, error) {
	if len(validation.IsDNS1123Subdomain(clusterName)) != 0 {
		return "", fmt.Errorf("%w: cluster name", ErrInvalidClusterIdentity)
	}
	if parsed, err := uuid.Parse(clusterID); err != nil || parsed == uuid.Nil() {
		return "", fmt.Errorf("%w: cluster id", ErrInvalidClusterIdentity)
	}
	if generation < 1 {
		return "", ErrInvalidBundleGeneration
	}
	return clusterName + "-talos-secrets-" + clusterID + "-g" + strconv.FormatInt(int64(generation), 10), nil
}

// BuildPendingSecretはTalos machineryが生成したcomplete bundleをPendingとして永続化する。
// この関数は秘密materialを生成せず、入力mapをSecretへ安全にcloneするだけである。
func BuildPendingSecret(namespace, clusterName, clusterID string, generation int32, owner metav1.OwnerReference, data map[string][]byte) (*corev1.Secret, error) {
	name, err := BundleName(clusterName, clusterID, generation)
	if err != nil {
		return nil, err
	}
	if namespace == "" || owner.APIVersion == "" || owner.Kind == "" || owner.Name == "" || owner.UID == "" {
		return nil, ErrBundleOwnerIncomplete
	}
	cloned, err := cloneCompleteData(data)
	if err != nil {
		return nil, err
	}
	controller := true
	return &corev1.Secret{
		Name:      name,
		Namespace: namespace,
		Labels: map[string]string{
			ClusterNameLabel: clusterName,
			ClusterIDLabel:   clusterID,
			GenerationLabel:  strconv.FormatInt(int64(generation), 10),
			BundleStateLabel: BundleStatePending,
		},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: owner.APIVersion,
			Kind:       owner.Kind,
			Name:       owner.Name,
			UID:        owner.UID,
			Controller: &controller,
		}},
		Type:      corev1.SecretTypeOpaque,
		Immutable: new(true),
		Data:      cloned,
	}, nil
}

// RotateDataは指定したrotation対象keyだけを差し替えた完全な次世代bundleを返す。
// 対象外keyの値はbyte単位で維持し、partial bundleや余分なreplacementを拒否する。
func RotateData(previous, replacements map[string][]byte, rotationKeys []string) (map[string][]byte, error) {
	cloned, err := cloneCompleteData(previous)
	if err != nil {
		return nil, err
	}
	if len(rotationKeys) == 0 || len(replacements) != len(rotationKeys) {
		return nil, ErrRotationTargetMismatch
	}
	for _, key := range rotationKeys {
		value, ok := replacements[key]
		if !ok || len(value) == 0 {
			return nil, ErrRotationTargetMismatch
		}
		if _, ok := previous[key]; !ok {
			return nil, ErrRotationTargetMismatch
		}
		cloned[key] = bytes.Clone(value)
	}
	return cloned, nil
}

func cloneCompleteData(data map[string][]byte) (map[string][]byte, error) {
	if len(data) == 0 {
		return nil, ErrBundleDataIncomplete
	}
	cloned := maps.Clone(data)
	for key, value := range cloned {
		if key == "" || len(value) == 0 {
			return nil, ErrBundleDataIncomplete
		}
		cloned[key] = bytes.Clone(value)
	}
	return cloned, nil
}
