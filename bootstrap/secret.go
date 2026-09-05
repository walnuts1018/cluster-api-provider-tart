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
	BootstrapSecretType     = corev1.SecretType("cluster.x-k8s.io/secret")
	BootstrapSecretKey      = "value"
	ClusterNameLabel        = "cluster.x-k8s.io/cluster-name"
	ConfigurationInputKey   = "value"
	ConfigurationPatchesKey = "patches"
)

var (
	ErrMissingConfigSecret         = errors.New("bootstrap configuration Secret is missing")
	ErrConfigSecretNotImmutable    = errors.New("bootstrap configuration Secret must be immutable")
	ErrConfigSecretEmpty           = errors.New("bootstrap configuration Secret has no data")
	ErrInvalidSecretMetadata       = errors.New("bootstrap Secret metadata is incomplete")
	ErrOwnerReferenceIncomplete    = errors.New("bootstrap Secret owner reference is incomplete")
	ErrOwnerReferenceInvalid       = errors.New("bootstrap Secret owner reference is invalid")
	ErrCompleteConfigurationEmpty  = errors.New("complete machine configuration is empty")
	ErrConfigurationInputMissing   = errors.New("bootstrap configuration Secret has no usable configuration input")
	ErrConfigurationInputAmbiguous = errors.New("bootstrap configuration Secret contains both complete configuration and patches")
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

// CompleteConfigurationFromSecretはimmutableな入力Secretから完全なTalos configurationを取得する。
// patches keyの入力はBootstrap controllerがcluster、Machine、bundle contextを解決してからGenerateMachineConfigurationへ渡す。
func CompleteConfigurationFromSecret(secret *corev1.Secret) ([]byte, error) {
	if err := ValidateConfigSecret(secret); err != nil {
		return nil, err
	}
	if len(secret.Data[ConfigurationInputKey]) > 0 && len(secret.Data[ConfigurationPatchesKey]) > 0 {
		return nil, ErrConfigurationInputAmbiguous
	}

	configuration, ok := secret.Data[ConfigurationInputKey]
	if !ok {
		if len(secret.Data) != 1 {
			return nil, ErrConfigurationInputMissing
		}
		for _, value := range secret.Data {
			configuration = value
		}
	}

	rendered, err := RenderEffectiveConfiguration(configuration)
	if err != nil {
		return nil, fmt.Errorf("render configuration input: %w", err)
	}

	return rendered, nil
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
