// Package certbuilderはTartCluster secret bundleに関するsiderolabs machinery型の実際の操作を提供する。
// domain/controlplaneの純粋関数はSecret契約・世代遷移・quorum・CA rotation段階だけを扱い、Talos
// secrets.Bundleの生成・decode・rotationはこのpackageへ閉じ込める。
package certbuilder

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"go.yaml.in/yaml/v4"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"

	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
	domaincontrolplane "github.com/walnuts1018/cluster-api-provider-tart/domain/controlplane"
)

var (
	// ErrCertificateAuthorityMissingはTalos secrets bundleがrotation対象CAの生成に必要な要素を欠く場合に返す。
	ErrCertificateAuthorityMissing = errors.New("secret bundle is missing a rotation certificate authority")
	// ErrBundleIdentityMismatchはSecretから復号したbundleのcluster identityが期待値と一致しない場合に返す。
	ErrBundleIdentityMismatch = errors.New("bundle cluster identity does not match")
	// ErrCATrustConfigurationIncompleteはTalos machine configurationにCA rotation判定に必要なdocumentが無い場合に返す。
	ErrCATrustConfigurationIncomplete = errors.New("talos machine configuration has no CA rotation documents")
)

// GenerateBundleDataはTalos machineryで新規cluster-level secret bundleを生成する。
func GenerateBundleData(clusterID clusterdomain.ClusterID) (map[string][]byte, error) {
	if clusterID.IsZero() {
		return nil, fmt.Errorf("%w: cluster id", domaincontrolplane.ErrInvalidClusterIdentity)
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

	return map[string][]byte{domaincontrolplane.BundleDataKey: encoded}, nil
}

// ValidateBundleDataはSecret内のbundleが指定されたCluster identityの完全なTalos bundleか確認する。
func ValidateBundleData(data map[string][]byte, clusterID clusterdomain.ClusterID) error {
	_, err := DecodeBundleData(data, clusterID)
	return err
}

// DecodeBundleDataはSecret内のbundleを検証し、Talos machineryが利用できる形で返す。
func DecodeBundleData(data map[string][]byte, clusterID clusterdomain.ClusterID) (*secrets.Bundle, error) {
	if clusterID.IsZero() {
		return nil, domaincontrolplane.ErrInvalidClusterIdentity
	}
	encoded, ok := data[domaincontrolplane.BundleDataKey]
	if !ok || len(encoded) == 0 || len(data) != 1 {
		return nil, domaincontrolplane.ErrBundleDataIncomplete
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
		return nil, fmt.Errorf("%w: cluster id", domaincontrolplane.ErrInvalidClusterIdentity)
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

	return map[string][]byte{domaincontrolplane.BundleDataKey: encoded}, nil
}

// ExtractRotationCertificateAuthoritiesはTalos secrets bundleからrotation対象のCAを取り出す。
// etcd CAはTalosがaccepted-CAによる二重信頼をサポートしないため、rotation対象に含めない。
func ExtractRotationCertificateAuthorities(bundle *secrets.Bundle) (domaincontrolplane.CertBundle, error) {
	if bundle == nil || bundle.Certs == nil || bundle.Certs.OS == nil || bundle.Certs.K8s == nil || bundle.Certs.K8sAggregator == nil {
		return domaincontrolplane.CertBundle{}, ErrCertificateAuthorityMissing
	}
	return domaincontrolplane.CertBundle{
		Machine:              bundle.Certs.OS,
		KubernetesAPI:        bundle.Certs.K8s,
		KubernetesAggregator: bundle.Certs.K8sAggregator,
	}, nil
}

// ObserveCATrustStageはTalosから読み出した稼働中machine configurationのissuing/accepted CAを、active/pending bundleのrotation対象CAと比較して進行段階を判定する。
func ObserveCATrustStage(configuration []byte, active, pending domaincontrolplane.CertBundle) (domaincontrolplane.CATrustStage, error) {
	if len(bytes.TrimSpace(configuration)) == 0 {
		return domaincontrolplane.CATrustStageUnknown, errors.New("talos machine configuration is empty")
	}
	provider, err := configloader.NewFromBytes(configuration)
	if err != nil {
		return domaincontrolplane.CATrustStageUnknown, fmt.Errorf("load talos machine configuration: %w", err)
	}
	if provider.Machine() == nil || provider.Machine().Security() == nil {
		return domaincontrolplane.CATrustStageUnknown, fmt.Errorf("%w: machine security", ErrCATrustConfigurationIncomplete)
	}
	apiConfig := provider.K8sAPIServerCAConfig()
	aggregatorConfig := provider.K8sAggregatorCAConfig()
	if apiConfig == nil || aggregatorConfig == nil {
		return domaincontrolplane.CATrustStageUnknown, fmt.Errorf("%w: Kubernetes API/aggregator CA", ErrCATrustConfigurationIncomplete)
	}
	security := provider.Machine().Security()

	return domaincontrolplane.ObserveStage(
		security.IssuingCA(), security.AcceptedCAs(),
		apiConfig.IssuingCA(), apiConfig.AcceptedCAs(),
		aggregatorConfig.IssuingCA(), aggregatorConfig.AcceptedCAs(),
		active, pending,
	), nil
}
