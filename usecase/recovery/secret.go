package recovery

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/siderolabs/crypto/x509"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/role"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
)

const (
	// ClusterIDLabelはrecovery Secretが属するTalos cluster identityを示す。
	ClusterIDLabel = "tart.cluster.x-k8s.io/cluster-id"
	// SecretTypeLabelはprovider管理namespace上のSecretのうち、recovery identityを保持するものだけをGC対象として識別する。
	SecretTypeLabel = "tart.cluster.x-k8s.io/secret-type"
	// SecretTypeRecoveryはSecretTypeLabelの値である。
	SecretTypeRecovery = "talos-recovery"

	// CACertificateKeyはTalos API(machine/OS) CAのcertificateを保持するSecret keyである。
	CACertificateKey = "os-ca.crt"
	// CAKeyKeyはTalos API(machine/OS) CAのprivate keyを保持するSecret keyである。short-lived client certificateの再発行にだけ使う。
	CAKeyKey = "os-ca.key"
	// ClusterIDKeyはこのrecovery identityが表すTalos cluster IDを保持するSecret keyである。
	ClusterIDKey = "clusterID"

	// SecretNamePrefixはrecovery Secret名の固定接頭辞である。
	SecretNamePrefix = "tart-talos-recovery-"

	// ClientCertificateTTLはReset operationを完了するのに十分な範囲へ限定した、recovery CAから発行する短命client certificateの有効期間である。
	ClientCertificateTTL = 10 * time.Minute
)

var (
	// ErrInvalidClusterIdentityはrecovery Secretが表すcluster identityを決定できないことを示す。
	ErrInvalidClusterIdentity = errors.New("invalid Talos recovery cluster identity")
	// ErrSecretInvalidはSecretがrecovery Secret契約を満たさないことを示す。
	ErrSecretInvalid = errors.New("talos recovery Secret does not satisfy its contract")
	// ErrCertificateAuthorityMissingはrecovery materialにTalos API CAが含まれないことを示す。
	ErrCertificateAuthorityMissing = errors.New("talos recovery material is missing the Talos API certificate authority")
)

// SecretNameはTalos cluster IDと、その世代のTalos API CA certificateのfingerprintからrecovery Secretの決定論的な名前を返す。
// 同じ旧Talos clusterかつ同じCA generationに属するHostは1つのrecovery Secretを共有し、HostごとにCA private keyを複製しない。
// CA rotationで新しいCAが有効になった場合は別のSecretとなり、旧CAのSecretは旧installationを保持するHostが参照する間だけ残る。
func SecretName(clusterID string, certificateAuthority *x509.PEMEncodedCertificateAndKey) (string, error) {
	parsed, err := clusterdomain.ParseClusterID(clusterID)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidClusterIdentity, err)
	}
	fingerprint, err := CertificateAuthorityFingerprint(certificateAuthority)
	if err != nil {
		return "", err
	}
	name := SecretNamePrefix + parsed.String() + "-" + fingerprint
	if len(validation.IsDNS1123Subdomain(name)) != 0 {
		return "", fmt.Errorf("%w: generated Secret name", ErrInvalidClusterIdentity)
	}
	return name, nil
}

// CertificateAuthorityFingerprintはTalos API CA certificateの安定した短い識別子を返す。private keyは入力にせず、名前へ機密値を漏らさない。
func CertificateAuthorityFingerprint(certificateAuthority *x509.PEMEncodedCertificateAndKey) (string, error) {
	if certificateAuthority == nil || len(bytes.TrimSpace(certificateAuthority.Crt)) == 0 {
		return "", ErrCertificateAuthorityMissing
	}
	digest := sha256.Sum256(bytes.TrimSpace(certificateAuthority.Crt))
	return hex.EncodeToString(digest[:8]), nil
}

// MaterialはRetained Hostへ再接続するために必要な最小限のTalos PKI materialである。
// Kubernetes PKI、service account key、bootstrap token、Bootstrap Data全体は保持しない。
type Material struct {
	ClusterID string
	// CertificateAuthorityはTalos API(machine/OS) CAのcertificateとprivate keyである。短命client certificateの再発行にだけ使う。
	CertificateAuthority *x509.PEMEncodedCertificateAndKey
}

// MaterialFromBundleはcluster secret bundleから、recovery identityとして保持する最小限のmaterialだけを抽出する。
// bundleにはKubernetes PKIやbootstrap tokenも含まれるが、Talos API CA以外は複製しない。
func MaterialFromBundle(bundle *secrets.Bundle) (Material, error) {
	if bundle == nil || bundle.Cluster == nil || strings.TrimSpace(bundle.Cluster.ID) == "" {
		return Material{}, ErrInvalidClusterIdentity
	}
	if bundle.Certs == nil || bundle.Certs.OS == nil || len(bundle.Certs.OS.Crt) == 0 || len(bundle.Certs.OS.Key) == 0 {
		return Material{}, ErrCertificateAuthorityMissing
	}
	clusterID, err := clusterdomain.ParseClusterID(bundle.Cluster.ID)
	if err != nil {
		return Material{}, fmt.Errorf("%w: %w", ErrInvalidClusterIdentity, err)
	}
	return Material{
		ClusterID: clusterID.String(),
		CertificateAuthority: &x509.PEMEncodedCertificateAndKey{
			Crt: bytes.Clone(bundle.Certs.OS.Crt),
			Key: bytes.Clone(bundle.Certs.OS.Key),
		},
	}, nil
}

// ObservedClusterIDは稼働中nodeのactive machine configurationから、そのnodeが属するTalos cluster IDだけを読み取る。
// worker configurationにはCA private keyもKubernetes PKIも含まれないため、identity照合に必要な値だけを取り出す。
func ObservedClusterID(configuration []byte) (string, error) {
	if len(bytes.TrimSpace(configuration)) == 0 {
		return "", errors.New("talos machine configuration is empty")
	}
	provider, err := configloader.NewFromBytes(configuration)
	if err != nil {
		return "", fmt.Errorf("load talos machine configuration: %w", err)
	}
	identity := provider.DiscoveryIdentityConfig()
	if identity == nil {
		return "", ErrInvalidClusterIdentity
	}
	clusterID, err := clusterdomain.ParseClusterID(identity.ClusterID())
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidClusterIdentity, err)
	}
	return clusterID.String(), nil
}

// BuildSecretはrecovery materialをprovider管理namespace上のimmutable Secretとして表現する。
// TartClusterやTartMachineのOwnerReferenceを付けず、GCの可否はTartHostの参照をreconcileで観測して判断する。
func BuildSecret(namespace string, material Material) (*corev1.Secret, error) {
	if strings.TrimSpace(namespace) == "" {
		return nil, fmt.Errorf("%w: namespace is empty", ErrSecretInvalid)
	}
	if material.CertificateAuthority == nil || len(material.CertificateAuthority.Crt) == 0 || len(material.CertificateAuthority.Key) == 0 {
		return nil, ErrCertificateAuthorityMissing
	}
	name, err := SecretName(material.ClusterID, material.CertificateAuthority)
	if err != nil {
		return nil, err
	}
	return &corev1.Secret{
		Name:      name,
		Namespace: namespace,
		Labels: map[string]string{
			ClusterIDLabel:  material.ClusterID,
			SecretTypeLabel: SecretTypeRecovery,
		},
		Type:      corev1.SecretTypeOpaque,
		Immutable: new(true),
		Data: map[string][]byte{
			ClusterIDKey:     []byte(material.ClusterID),
			CACertificateKey: bytes.Clone(material.CertificateAuthority.Crt),
			CAKeyKey:         bytes.Clone(material.CertificateAuthority.Key),
		},
	}, nil
}

// IsRecoverySecretはSecretがrecovery Secretとしてlabel付けされているかを返す。GC対象の絞り込みに使う。
func IsRecoverySecret(secret *corev1.Secret) bool {
	return secret != nil && secret.Labels[SecretTypeLabel] == SecretTypeRecovery
}

// DecodeSecretはrecovery Secretの契約を検証し、Talos API CAとcluster identityを取り出す。
// 呼び出し側は返却されたmaterialをStatus、Event、log、metricsへ出力してはならない。
func DecodeSecret(secret *corev1.Secret, expectedClusterID string) (Material, error) {
	if secret == nil || !IsRecoverySecret(secret) || secret.Type != corev1.SecretTypeOpaque {
		return Material{}, ErrSecretInvalid
	}
	if secret.Immutable == nil || !*secret.Immutable {
		return Material{}, ErrSecretInvalid
	}
	clusterID := strings.TrimSpace(string(secret.Data[ClusterIDKey]))
	parsed, err := clusterdomain.ParseClusterID(clusterID)
	if err != nil {
		return Material{}, fmt.Errorf("%w: %w", ErrSecretInvalid, err)
	}
	if secret.Labels[ClusterIDLabel] != parsed.String() {
		return Material{}, ErrSecretInvalid
	}
	if expected := strings.TrimSpace(expectedClusterID); expected != "" && expected != parsed.String() {
		return Material{}, ErrSecretInvalid
	}
	certificate := secret.Data[CACertificateKey]
	key := secret.Data[CAKeyKey]
	if len(bytes.TrimSpace(certificate)) == 0 || len(bytes.TrimSpace(key)) == 0 {
		return Material{}, ErrCertificateAuthorityMissing
	}
	material := Material{
		ClusterID: parsed.String(),
		CertificateAuthority: &x509.PEMEncodedCertificateAndKey{
			Crt: bytes.Clone(certificate),
			Key: bytes.Clone(key),
		},
	}
	// Secret名はcluster identityとCA certificateから決まるため、名前と内容の食い違いを検出できる。
	expectedName, err := SecretName(material.ClusterID, material.CertificateAuthority)
	if err != nil {
		return Material{}, err
	}
	if secret.Name != expectedName {
		return Material{}, ErrSecretInvalid
	}
	return material, nil
}

// ClientCertificateはrecovery CAから、Reset operationを完了するのに十分なだけの短命な`os:admin` client certificateを発行する。
// Talos machine APIのReset RPCはos:admin roleを要求するため、常設のadmin client keyをSecretへ置かず、必要時に都度発行する。
func (m Material) ClientCertificate(ttl time.Duration) (*x509.PEMEncodedCertificateAndKey, error) {
	if m.CertificateAuthority == nil || len(m.CertificateAuthority.Crt) == 0 || len(m.CertificateAuthority.Key) == 0 {
		return nil, ErrCertificateAuthorityMissing
	}
	if ttl <= 0 || ttl > ClientCertificateTTL {
		ttl = ClientCertificateTTL
	}
	bundle := &secrets.Bundle{
		Clock: secrets.NewFixedClock(time.Now()),
		Certs: &secrets.Certs{OS: m.CertificateAuthority},
	}
	certificate, err := bundle.GenerateTalosAPIClientCertificateWithTTL(role.MakeSet(role.Admin), ttl)
	if err != nil {
		return nil, fmt.Errorf("generate short-lived Talos API client certificate: %w", err)
	}
	return certificate, nil
}
