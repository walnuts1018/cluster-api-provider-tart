package controlplane

import (
	"fmt"
	"time"

	"go.yaml.in/yaml/v4"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
	domaincontrolplane "github.com/walnuts1018/cluster-api-provider-tart/domain/controlplane"
)

const (
	ClusterNameLabel   = domaincontrolplane.ClusterNameLabel
	ClusterIDLabel     = domaincontrolplane.ClusterIDLabel
	GenerationLabel    = domaincontrolplane.GenerationLabel
	BundleStateLabel   = domaincontrolplane.BundleStateLabel
	BundleStatePending = domaincontrolplane.BundleStatePending
	BundleStateActive  = domaincontrolplane.BundleStateActive
	BundleDataKey      = domaincontrolplane.BundleDataKey
)

var (
	ErrInvalidClusterIdentity      = domaincontrolplane.ErrInvalidClusterIdentity
	ErrInvalidBundleGeneration     = domaincontrolplane.ErrInvalidBundleGeneration
	ErrBundleDataIncomplete        = domaincontrolplane.ErrBundleDataIncomplete
	ErrRotationTargetMismatch      = domaincontrolplane.ErrRotationTargetMismatch
	ErrBundleOwnerIncomplete       = domaincontrolplane.ErrBundleOwnerIncomplete
	ErrBundleOwnerInvalid          = domaincontrolplane.ErrBundleOwnerInvalid
	ErrBundleSecretInvalid         = domaincontrolplane.ErrBundleSecretInvalid
	ErrBundleIdentityMismatch      = fmt.Errorf("bundle cluster identity does not match")
	ErrCertificateAuthorityMissing = fmt.Errorf("secret bundle is missing a rotation certificate authority")
)

// NextGenerationはdomain/controlplaneの純粋関数へ委譲する。
func NextGeneration(current int32) (int32, error) {
	return domaincontrolplane.NextGeneration(current)
}

// BundleNameはdomain/controlplaneの純粋関数へ委譲する。
func BundleName(clusterName string, clusterID clusterdomain.ClusterID, generation int32) (string, error) {
	return domaincontrolplane.BundleName(clusterName, clusterID, generation)
}

// ValidateBundleSecretContractはdomain/controlplaneの純粋関数へ委譲する。
func ValidateBundleSecretContract(secret *corev1.Secret, namespace, clusterName string, clusterID clusterdomain.ClusterID, generation int32, state string, ownerUID types.UID) error {
	return domaincontrolplane.ValidateBundleSecretContract(secret, namespace, clusterName, clusterID, generation, state, ownerUID)
}

// BuildPendingSecretはdomain/controlplaneの純粋関数へ委譲する。
func BuildPendingSecret(namespace, clusterName string, clusterID clusterdomain.ClusterID, generation int32, owner metav1.OwnerReference, data map[string][]byte) (*corev1.Secret, error) {
	return domaincontrolplane.BuildPendingSecret(namespace, clusterName, clusterID, generation, owner, data)
}

// BuildActiveSecretはdomain/controlplaneの純粋関数へ委譲する。
func BuildActiveSecret(namespace, clusterName string, clusterID clusterdomain.ClusterID, generation int32, owner metav1.OwnerReference, data map[string][]byte) (*corev1.Secret, error) {
	return domaincontrolplane.BuildActiveSecret(namespace, clusterName, clusterID, generation, owner, data)
}

// RotateDataはdomain/controlplaneの純粋関数へ委譲する。
func RotateData(previous, replacements map[string][]byte, rotationKeys []string) (map[string][]byte, error) {
	return domaincontrolplane.RotateData(previous, replacements, rotationKeys)
}

// RotationCertificateAuthoritiesはCA rotationで切り替える対象の各CAを保持する。
// etcd CAはTalosが accepted-CAによる二重信頼をサポートしないため、rotation対象に含めない。
type RotationCertificateAuthorities = domaincontrolplane.CertBundle

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

// GenerateBundleDataはTalos machineryでcluster-level secret bundleを生成する。
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

// GenerateRotatedBundleDataは既存bundleのcluster identity、token、TrustdInfoを保持したまま、CA rotation用に新しいmachine/Kubernetes API/aggregator CAだけを生成した次世代bundleを返す。
func GenerateRotatedBundleData(clusterID clusterdomain.ClusterID, previous *secrets.Bundle) (map[string][]byte, error) {
	if clusterID.IsZero() || previous == nil || previous.Cluster == nil || previous.Secrets == nil || previous.TrustdInfo == nil || previous.Certs == nil {
		return nil, fmt.Errorf("%w: cluster id", ErrInvalidClusterIdentity)
	}

	rotated, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		return nil, fmt.Errorf("generate rotated Talos secret bundle: %w", err)
	}
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
