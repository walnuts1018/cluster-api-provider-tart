package controlplane

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"time"

	"go.yaml.in/yaml/v4"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/siderolabs/crypto/x509"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
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
	ErrInvalidClusterIdentity      = errors.New("invalid cluster identity")
	ErrInvalidBundleGeneration     = errors.New("invalid bundle generation")
	ErrBundleDataIncomplete        = errors.New("bundle data is incomplete")
	ErrRotationTargetMismatch      = errors.New("rotation target mismatch")
	ErrBundleOwnerIncomplete       = errors.New("bundle owner reference is incomplete")
	ErrBundleOwnerInvalid          = errors.New("bundle owner reference is invalid")
	ErrBundleSecretInvalid         = errors.New("bundle Secret does not satisfy its contract")
	ErrBundleIdentityMismatch      = errors.New("bundle cluster identity does not match")
	ErrCertificateAuthorityMissing = errors.New("secret bundle is missing a rotation certificate authority")
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
func BundleName(clusterName string, clusterID clusterdomain.ClusterID, generation int32) (string, error) {
	if len(validation.IsDNS1123Subdomain(clusterName)) != 0 {
		return "", fmt.Errorf("%w: cluster name", ErrInvalidClusterIdentity)
	}
	if clusterID.IsZero() {
		return "", fmt.Errorf("%w: cluster id", ErrInvalidClusterIdentity)
	}
	if generation < 1 {
		return "", ErrInvalidBundleGeneration
	}
	name := clusterName + "-talos-secrets-" + clusterID.String() + "-g" + strconv.FormatInt(int64(generation), 10)
	if len(validation.IsDNS1123Subdomain(name)) != 0 {
		return "", fmt.Errorf("%w: generated Secret name", ErrInvalidClusterIdentity)
	}

	return name, nil
}

// ValidateBundleSecretContractは既存bundle Secretのidentityとmetadataを検証する。
// Secret dataの値はerrorへ含めず、Cluster IDとgenerationが一致しないbundleを再利用しない。
func ValidateBundleSecretContract(secret *corev1.Secret, namespace, clusterName string, clusterID clusterdomain.ClusterID, generation int32, state string, ownerUID types.UID) error {
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
	if secret.Labels[ClusterNameLabel] != clusterName || secret.Labels[ClusterIDLabel] != clusterID.String() || secret.Labels[GenerationLabel] != strconv.FormatInt(int64(generation), 10) || secret.Labels[BundleStateLabel] != state {
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
func BuildPendingSecret(namespace, clusterName string, clusterID clusterdomain.ClusterID, generation int32, owner metav1.OwnerReference, data map[string][]byte) (*corev1.Secret, error) {
	return buildBundleSecret(namespace, clusterName, clusterID, generation, BundleStatePending, owner, data)
}

// BuildActiveSecretは初回生成済みのcomplete bundleをActiveとして永続化するSecretを作成する。
func BuildActiveSecret(namespace, clusterName string, clusterID clusterdomain.ClusterID, generation int32, owner metav1.OwnerReference, data map[string][]byte) (*corev1.Secret, error) {
	return buildBundleSecret(namespace, clusterName, clusterID, generation, BundleStateActive, owner, data)
}

func buildBundleSecret(namespace, clusterName string, clusterID clusterdomain.ClusterID, generation int32, state string, owner metav1.OwnerReference, data map[string][]byte) (*corev1.Secret, error) {
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
			ClusterIDLabel:   clusterID.String(),
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
func GenerateBundleData(clusterID clusterdomain.ClusterID) (map[string][]byte, error) {
	if clusterID.IsZero() {
		return nil, fmt.Errorf("%w: cluster id", ErrInvalidClusterIdentity)
	}

	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		return nil, fmt.Errorf("generate Talos secret bundle: %w", err)
	}
	bundle.Cluster.ID = clusterID.String()
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
func ValidateBundleData(data map[string][]byte, clusterID clusterdomain.ClusterID) error {
	_, err := DecodeBundleData(data, clusterID)

	return err
}

// DecodeBundleDataはSecret内のbundleを検証し、Talos machineryが利用できる形で返す。
// 呼び出し側は返却されたbundleをStatus、Event、logへ出力してはならない。
func DecodeBundleData(data map[string][]byte, clusterID clusterdomain.ClusterID) (*secrets.Bundle, error) {
	if clusterID.IsZero() {
		return nil, ErrInvalidClusterIdentity
	}
	encoded, ok := data[BundleDataKey]
	if !ok || len(encoded) == 0 || len(data) != 1 {
		return nil, ErrBundleDataIncomplete
	}

	var bundle secrets.Bundle
	if err := yaml.Unmarshal(encoded, &bundle); err != nil {
		return nil, fmt.Errorf("unmarshal Talos secret bundle: %w", err)
	}
	if bundle.Cluster == nil || bundle.Cluster.ID != clusterID.String() {
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

// RotationCertificateAuthoritiesはCA rotationで切り替える対象の各CA(machine/OS、Kubernetes API server、Kubernetes aggregator)を保持する。
// etcd CAはTalosが accepted-CAによる二重信頼をサポートしないため、rotation対象に含めない。
type RotationCertificateAuthorities struct {
	Machine              *x509.PEMEncodedCertificateAndKey
	KubernetesAPI        *x509.PEMEncodedCertificateAndKey
	KubernetesAggregator *x509.PEMEncodedCertificateAndKey
}

// ExtractRotationCertificateAuthoritiesはTalos secrets bundleからrotation対象のCAを取り出す。
func ExtractRotationCertificateAuthorities(bundle *secrets.Bundle) (RotationCertificateAuthorities, error) {
	if bundle == nil || bundle.Certs == nil || bundle.Certs.OS == nil || bundle.Certs.K8s == nil || bundle.Certs.K8sAggregator == nil {
		return RotationCertificateAuthorities{}, ErrCertificateAuthorityMissing
	}
	return RotationCertificateAuthorities{
		Machine:              bundle.Certs.OS,
		KubernetesAPI:        bundle.Certs.K8s,
		KubernetesAggregator: bundle.Certs.K8sAggregator,
	}, nil
}

// GenerateRotatedBundleDataは既存bundleのcluster identity、token、TrustdInfoを保持したまま、CA rotation用に新しいmachine/Kubernetes API/aggregator CAだけを生成した次世代bundleを返す。
// etcd CAとjoin tokenは維持するため、既存clusterのmembershipやbootstrapに影響しない。
func GenerateRotatedBundleData(clusterID clusterdomain.ClusterID, previous *secrets.Bundle) (map[string][]byte, error) {
	if clusterID.IsZero() || previous == nil || previous.Cluster == nil || previous.Secrets == nil || previous.TrustdInfo == nil || previous.Certs == nil {
		return nil, fmt.Errorf("%w: cluster id", ErrInvalidClusterIdentity)
	}

	rotated, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		return nil, fmt.Errorf("generate rotated Talos secret bundle: %w", err)
	}
	// Cluster identity、bootstrap token、trustd tokenは維持し、CA(Certs)だけを新しく生成した値へ差し替える。
	// etcd CAは維持し、rotation対象外とする。
	rotated.Cluster = previous.Cluster
	rotated.Cluster.ID = clusterID.String()
	rotated.Secrets = previous.Secrets
	rotated.TrustdInfo = previous.TrustdInfo
	rotated.Certs.Etcd = previous.Certs.Etcd
	if err := rotated.Validate(talosconfig.TalosVersionCurrent); err != nil {
		return nil, fmt.Errorf("validate rotated Talos secret bundle: %w", err)
	}
	encoded, err := yaml.Marshal(rotated)
	if err != nil {
		return nil, fmt.Errorf("marshal rotated Talos secret bundle: %w", err)
	}

	return map[string][]byte{BundleDataKey: encoded}, nil
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
