package controlplane

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"time"
	"uuid"

	"go.yaml.in/yaml/v4"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

const (
	ClusterNameLabel   = "cluster.x-k8s.io/cluster-name"
	ClusterIDLabel     = "tart.cluster.x-k8s.io/cluster-id"
	GenerationLabel    = "tart.cluster.x-k8s.io/secret-generation"
	BundleStateLabel   = "tart.cluster.x-k8s.io/bundle-state"
	BundleStatePending = "Pending"
	BundleStateActive  = "Active"
	BundleDataKey      = "bundle"
)

var (
	ErrInvalidClusterIdentity  = errors.New("invalid cluster identity")
	ErrInvalidBundleGeneration = errors.New("invalid bundle generation")
	ErrBundleDataIncomplete    = errors.New("bundle data is incomplete")
	ErrRotationTargetMismatch  = errors.New("rotation target mismatch")
	ErrBundleOwnerIncomplete   = errors.New("bundle owner reference is incomplete")
	ErrBundleOwnerInvalid      = errors.New("bundle owner reference is invalid")
	ErrBundleSecretInvalid     = errors.New("bundle Secret does not satisfy its contract")
	ErrBundleIdentityMismatch  = errors.New("bundle cluster identity does not match")
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
	name := clusterName + "-talos-secrets-" + clusterID + "-g" + strconv.FormatInt(int64(generation), 10)
	if len(validation.IsDNS1123Subdomain(name)) != 0 {
		return "", fmt.Errorf("%w: generated Secret name", ErrInvalidClusterIdentity)
	}

	return name, nil
}

// ValidateBundleSecretContractは既存bundle Secretのidentityとmetadataを検証する。
// Secret dataの値はerrorへ含めず、Cluster IDとgenerationが一致しないbundleを再利用しない。
func ValidateBundleSecretContract(secret *corev1.Secret, namespace, clusterName, clusterID string, generation int32, state string, ownerUID types.UID) error {
	if secret == nil || namespace == "" || secret.Namespace != namespace {
		return ErrBundleSecretInvalid
	}
	expectedName, err := BundleName(clusterName, clusterID, generation)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBundleSecretInvalid, err)
	}
	if secret.Name != expectedName || secret.Type != corev1.SecretTypeOpaque || secret.Immutable == nil || !*secret.Immutable {
		return ErrBundleSecretInvalid
	}
	if secret.Labels[ClusterNameLabel] != clusterName || secret.Labels[ClusterIDLabel] != clusterID || secret.Labels[GenerationLabel] != strconv.FormatInt(int64(generation), 10) || secret.Labels[BundleStateLabel] != state {
		return ErrBundleSecretInvalid
	}
	if state != BundleStatePending && state != BundleStateActive {
		return ErrBundleSecretInvalid
	}
	if _, err := cloneCompleteData(secret.Data); err != nil {
		return fmt.Errorf("%w: %w", ErrBundleSecretInvalid, err)
	}
	if len(secret.OwnerReferences) != 1 {
		return ErrBundleSecretInvalid
	}
	owner := secret.OwnerReferences[0]
	if owner.APIVersion != infrav1alpha1.GroupVersion.String() || owner.Kind != "TartCluster" || owner.Name != clusterName || owner.UID == "" || owner.Controller == nil || !*owner.Controller {
		return ErrBundleSecretInvalid
	}
	if ownerUID != "" && owner.UID != ownerUID {
		return ErrBundleSecretInvalid
	}

	return nil
}

// BuildPendingSecretはTalos machineryが生成したcomplete bundleをPendingとして永続化する。
// この関数は秘密materialを生成せず、入力mapをSecretへ安全にcloneするだけである。
func BuildPendingSecret(namespace, clusterName, clusterID string, generation int32, owner metav1.OwnerReference, data map[string][]byte) (*corev1.Secret, error) {
	return buildBundleSecret(namespace, clusterName, clusterID, generation, BundleStatePending, owner, data)
}

// BuildActiveSecretは初回生成済みのcomplete bundleをActiveとして永続化するSecretを作成する。
func BuildActiveSecret(namespace, clusterName, clusterID string, generation int32, owner metav1.OwnerReference, data map[string][]byte) (*corev1.Secret, error) {
	return buildBundleSecret(namespace, clusterName, clusterID, generation, BundleStateActive, owner, data)
}

func buildBundleSecret(namespace, clusterName, clusterID string, generation int32, state string, owner metav1.OwnerReference, data map[string][]byte) (*corev1.Secret, error) {
	name, err := BundleName(clusterName, clusterID, generation)
	if err != nil {
		return nil, err
	}
	if state != BundleStatePending && state != BundleStateActive {
		return nil, ErrBundleSecretInvalid
	}
	if namespace == "" || owner.APIVersion == "" || owner.Kind == "" || owner.Name == "" || owner.UID == "" {
		return nil, ErrBundleOwnerIncomplete
	}
	if owner.APIVersion != infrav1alpha1.GroupVersion.String() || owner.Kind != "TartCluster" || owner.Name != clusterName {
		return nil, ErrBundleOwnerInvalid
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
			BundleStateLabel: state,
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

// GenerateBundleDataはTalos machineryでcluster-level secret bundleを生成する。
// TartClusterのidentityをTalos bundleのcluster identityにも設定し、世代Secretのlabelと内容を同じClusterへ束ねる。
func GenerateBundleData(clusterID string) (map[string][]byte, error) {
	if _, err := uuid.Parse(clusterID); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidClusterIdentity, err)
	}

	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		return nil, fmt.Errorf("generate Talos secret bundle: %w", err)
	}
	bundle.Cluster.ID = clusterID
	if err := bundle.Validate(talosconfig.TalosVersionCurrent); err != nil {
		return nil, fmt.Errorf("validate generated Talos secret bundle: %w", err)
	}
	encoded, err := yaml.Marshal(bundle)
	if err != nil {
		return nil, fmt.Errorf("marshal Talos secret bundle: %w", err)
	}

	return map[string][]byte{BundleDataKey: encoded}, nil
}

// ValidateBundleDataはSecret内のbundleが指定されたCluster identityの完全なTalos bundleか確認する。
func ValidateBundleData(data map[string][]byte, clusterID string) error {
	_, err := DecodeBundleData(data, clusterID)

	return err
}

// DecodeBundleDataはSecret内のbundleを検証し、Talos machineryが利用できる形で返す。
// 呼び出し側は返却されたbundleをStatus、Event、logへ出力してはならない。
func DecodeBundleData(data map[string][]byte, clusterID string) (*secrets.Bundle, error) {
	if _, err := uuid.Parse(clusterID); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidClusterIdentity, err)
	}
	encoded, ok := data[BundleDataKey]
	if !ok || len(encoded) == 0 || len(data) != 1 {
		return nil, ErrBundleDataIncomplete
	}

	var bundle secrets.Bundle
	if err := yaml.Unmarshal(encoded, &bundle); err != nil {
		return nil, fmt.Errorf("unmarshal Talos secret bundle: %w", err)
	}
	if bundle.Cluster == nil || bundle.Cluster.ID != clusterID {
		return nil, ErrBundleIdentityMismatch
	}
	if err := bundle.Validate(talosconfig.TalosVersionCurrent); err != nil {
		return nil, fmt.Errorf("validate Talos secret bundle: %w", err)
	}

	return &bundle, nil
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
