package configbuilder

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	siderox509 "github.com/siderolabs/crypto/x509"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	coreconfig "github.com/siderolabs/talos/pkg/machinery/config/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	blockconfig "github.com/siderolabs/talos/pkg/machinery/config/types/block"
	storageconfig "github.com/siderolabs/talos/pkg/machinery/config/types/storage"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	domainupdate "github.com/walnuts1018/cluster-api-provider-tart/domain/update"
)

var ErrInvalidPKIMaterial = errors.New("invalid PKI material")

// normalizedInstallerImageは、installer image identityを比較対象から外すためのsentinel値である。
const (
	normalizedInstallerVersion   = "v0.0.0"
	normalizedInstallerSchematic = "normalized"
)

// ClassifyConfigurationChangeは2つのcomplete machine configurationの差分を分類する。
// configurationを読み込めない場合は判定できないためerrorを返し、呼び出し側はfail-closedで停止する。
func ClassifyConfigurationChange(active, desired []byte) (domainupdate.ChangeClass, string, error) {
	active, err := normalizeInstallerImage(active)
	if err != nil {
		return "", "", err
	}
	desired, err = normalizeInstallerImage(desired)
	if err != nil {
		return "", "", err
	}
	activeProvider, err := configloader.NewFromBytes(bytes.Clone(active))
	if err != nil {
		return "", "", fmt.Errorf("load active machine configuration: %w", err)
	}
	desiredProvider, err := configloader.NewFromBytes(bytes.Clone(desired))
	if err != nil {
		return "", "", fmt.Errorf("load desired machine configuration: %w", err)
	}
	activeDigest, err := DigestEffectiveConfiguration(active)
	if err != nil {
		return "", "", fmt.Errorf("digest active machine configuration: %w", err)
	}
	desiredDigest, err := DigestEffectiveConfiguration(desired)
	if err != nil {
		return "", "", fmt.Errorf("digest desired machine configuration: %w", err)
	}
	if activeDigest == desiredDigest {
		return domainupdate.ChangeNone, "", nil
	}
	if reason := invariantConflict(activeProvider, desiredProvider); reason != "" {
		return domainupdate.ChangeInvariantConflict, reason, nil
	}
	if reason := destructiveChange(activeProvider, desiredProvider); reason != "" {
		return domainupdate.ChangeReprovisionRequired, reason, nil
	}
	return domainupdate.ChangeUpdatable, "The machine configuration difference preserves node data and identity.", nil
}

// invariantConflictは、provider-ownedなcluster identity、PKI、endpoint、machine role、Kubernetes component version、
// ProviderIDの競合を検出する。競合はupdateとして適用せずfail-closedで停止するためのものである。
func invariantConflict(active, desired talosconfig.Provider) string {
	activeMachine, desiredMachine := active.Machine(), desired.Machine()
	if activeMachine == nil || desiredMachine == nil {
		return "The machine configuration does not contain a machine section; the update is stopped."
	}
	if activeMachine.Type() != desiredMachine.Type() {
		return "The machine role changed; the update is stopped."
	}
	if !sameCertificateAndKey(activeMachine.Security().IssuingCA(), desiredMachine.Security().IssuingCA()) || activeMachine.Security().Token() != desiredMachine.Security().Token() {
		return "The machine PKI or join token changed; the update is stopped."
	}
	activeCluster, desiredCluster := active.Cluster(), desired.Cluster()
	if activeCluster == nil || desiredCluster == nil {
		return "The machine configuration does not contain a cluster section; the update is stopped."
	}
	if activeCluster.Token().ID() != desiredCluster.Token().ID() || activeCluster.Token().Secret() != desiredCluster.Token().Secret() || !sameCertificateAndKey(activeCluster.Etcd().CA(), desiredCluster.Etcd().CA()) {
		return "The cluster identity, token, or etcd PKI changed; the update is stopped."
	}
	if !sameKubernetesPKI(active, desired) {
		return "The Kubernetes PKI changed; the update is stopped."
	}
	activeK8sCluster, desiredK8sCluster := active.K8sClusterConfig(), desired.K8sClusterConfig()
	if activeK8sCluster == nil || desiredK8sCluster == nil {
		return "The Kubernetes cluster configuration is unavailable; the update is stopped."
	}
	if activeK8sCluster.ClusterName() != desiredK8sCluster.ClusterName() || !sameEndpoint(activeK8sCluster.ClusterEndpoint(), desiredK8sCluster.ClusterEndpoint()) {
		return "The cluster name or control-plane endpoint changed; the update is stopped."
	}
	if componentImages(active) != componentImages(desired) {
		return "A Kubernetes component image changed; the update is stopped."
	}
	activeProviderID, activeProviderIDValid := providerID(active)
	desiredProviderID, desiredProviderIDValid := providerID(desired)
	if !activeProviderIDValid || !desiredProviderIDValid || activeProviderID != desiredProviderID {
		return "The kubelet ProviderID changed; the update is stopped."
	}
	return ""
}

// destructiveChangeは、disk layout、installation target、既存volumeのwipe、node identityを破壊する差分を検出する。
// 判定できないconfiguration document kindも安全側としてここで停止させる。
func destructiveChange(active, desired talosconfig.Provider) string {
	activeInstall, desiredInstall := active.UnattendedInstallConfig(), desired.UnattendedInstallConfig()
	if (activeInstall == nil) != (desiredInstall == nil) {
		return "The install configuration was added or removed; the Machine must be reprovisioned."
	}
	if activeInstall != nil && desiredInstall != nil {
		if !reflect.DeepEqual(activeInstall.VolumeSelector(), desiredInstall.VolumeSelector()) {
			return "The install disk selection changed; the Machine must be reprovisioned."
		}
		if activeInstall.VolumeWipe() != desiredInstall.VolumeWipe() {
			return "The install wipe setting changed; the Machine must be reprovisioned."
		}
	}
	if reason := destructiveStorageChange(active, desired); reason != "" {
		return reason
	}
	return ""
}

// storageDocumentKindsは、physical diskへのbindingやLVM layoutを保持し、destructiveな変更をここで
// 検出しなければならないTalos storage document kindである。VolumeConfig/UserVolumeConfigは
// EPHEMERALやLonghornなどのmounted local storage、RawVolumeConfigは裸のpartition、
// LVMVolumeGroupConfig/LVMLogicalVolumeConfigはTopoLVMのようなLVM layoutを表す。
var storageDocumentKinds = []string{
	blockconfig.VolumeConfigKind,
	blockconfig.UserVolumeConfigKind,
	blockconfig.RawVolumeConfigKind,
	storageconfig.LVMVolumeGroupConfigKind,
	storageconfig.LVMLogicalVolumeConfigKind,
}

// provisionedVolumeDocumentは、VolumeConfig/UserVolumeConfig/RawVolumeConfigに共通するprovisioning
// accessorである。
type provisionedVolumeDocument interface {
	coreconfig.NamedDocument
	Provisioning() coreconfig.VolumeProvisioningConfig
}

// destructiveStorageChangeは、physical diskをbindするstorage document(VolumeConfig、
// UserVolumeConfig、RawVolumeConfig、LVMVolumeGroupConfig、LVMLogicalVolumeConfig)の破壊的な
// 変更を検出する。documentの削除、disk/volume selectorの変更、sizeの縮小、LVM layoutの変更は
// 既存dataを破壊し得るため、常にreprovisionを要求する。documentの追加やsizeの拡大(grow)は
// 既存dataを保持するため許可する。
func destructiveStorageChange(active, desired talosconfig.Provider) string {
	activeDocs := indexNamedDocuments(active, storageDocumentKinds)
	desiredDocs := indexNamedDocuments(desired, storageDocumentKinds)

	for key, activeDoc := range activeDocs {
		desiredDoc, ok := desiredDocs[key]
		if !ok {
			return fmt.Sprintf("The storage document %q was removed; the Machine must be reprovisioned.", key)
		}
		if reason := destructiveStorageDocumentChange(activeDoc, desiredDoc); reason != "" {
			return reason
		}
	}
	return ""
}

func indexNamedDocuments(provider talosconfig.Provider, kinds []string) map[string]coreconfig.Document {
	index := make(map[string]coreconfig.Document)
	for _, doc := range provider.Documents() {
		named, ok := doc.(coreconfig.NamedDocument)
		if !ok {
			continue
		}
		for _, kind := range kinds {
			if doc.Kind() == kind {
				index[doc.Kind()+"/"+named.Name()] = doc
				break
			}
		}
	}
	return index
}

func destructiveStorageDocumentChange(active, desired coreconfig.Document) string {
	kind := active.Kind()
	switch kind {
	case blockconfig.VolumeConfigKind, blockconfig.UserVolumeConfigKind, blockconfig.RawVolumeConfigKind:
		activeVolume, activeOK := active.(provisionedVolumeDocument)
		desiredVolume, desiredOK := desired.(provisionedVolumeDocument)
		if !activeOK || !desiredOK {
			return fmt.Sprintf("The %s document is not a recognized storage document; the Machine must be reprovisioned.", kind)
		}
		return destructiveVolumeProvisioningChange(kind, activeVolume.Name(), activeVolume.Provisioning(), desiredVolume.Provisioning())
	case storageconfig.LVMVolumeGroupConfigKind:
		activeGroup, activeOK := active.(coreconfig.LVMVolumeGroupConfig)
		desiredGroup, desiredOK := desired.(coreconfig.LVMVolumeGroupConfig)
		if !activeOK || !desiredOK {
			return fmt.Sprintf("The %s document is not a recognized LVM volume group document; the Machine must be reprovisioned.", kind)
		}
		if activeGroup.PhysicalVolumeSelector().String() != desiredGroup.PhysicalVolumeSelector().String() {
			return fmt.Sprintf("The LVMVolumeGroupConfig %q physical volume selector changed; the Machine must be reprovisioned.", activeGroup.Name())
		}
		return ""
	case storageconfig.LVMLogicalVolumeConfigKind:
		activeVolume, activeOK := active.(coreconfig.LVMLogicalVolumeConfig)
		desiredVolume, desiredOK := desired.(coreconfig.LVMLogicalVolumeConfig)
		if !activeOK || !desiredOK {
			return fmt.Sprintf("The %s document is not a recognized LVM logical volume document; the Machine must be reprovisioned.", kind)
		}
		return destructiveLVMLogicalVolumeChange(activeVolume, desiredVolume)
	default:
		return fmt.Sprintf("The %s storage document kind is not recognized; the Machine must be reprovisioned.", kind)
	}
}

func destructiveVolumeProvisioningChange(kind, name string, active, desired coreconfig.VolumeProvisioningConfig) string {
	activeSelector, activeHasSelector := active.DiskSelector().Get()
	desiredSelector, desiredHasSelector := desired.DiskSelector().Get()
	if activeHasSelector != desiredHasSelector || (activeHasSelector && activeSelector.String() != desiredSelector.String()) {
		return fmt.Sprintf("The %s %q disk selector changed; the Machine must be reprovisioned.", kind, name)
	}
	activeMin, activeHasMin := active.MinSize().Get()
	desiredMin, desiredHasMin := desired.MinSize().Get()
	if activeHasMin && desiredHasMin && desiredMin < activeMin {
		return fmt.Sprintf("The %s %q minimum size shrank; the Machine must be reprovisioned.", kind, name)
	}
	activeMax, activeHasMax := active.MaxSize().Get()
	desiredMax, desiredHasMax := desired.MaxSize().Get()
	if activeHasMax && desiredHasMax && desiredMax < activeMax {
		return fmt.Sprintf("The %s %q maximum size shrank; the Machine must be reprovisioned.", kind, name)
	}
	if activeHasMax != desiredHasMax {
		return fmt.Sprintf("The %s %q size bound changed; the Machine must be reprovisioned.", kind, name)
	}
	return ""
}

func destructiveLVMLogicalVolumeChange(active, desired coreconfig.LVMLogicalVolumeConfig) string {
	name := active.Name()
	if active.VolumeGroup() != desired.VolumeGroup() {
		return fmt.Sprintf("The LVMLogicalVolumeConfig %q volume group changed; the Machine must be reprovisioned.", name)
	}
	if active.Type() != desired.Type() {
		return fmt.Sprintf("The LVMLogicalVolumeConfig %q layout type changed; the Machine must be reprovisioned.", name)
	}
	if active.Mirrors() != desired.Mirrors() || active.Stripes() != desired.Stripes() {
		return fmt.Sprintf("The LVMLogicalVolumeConfig %q mirror/stripe layout changed; the Machine must be reprovisioned.", name)
	}
	if desired.MinSizeBytes() < active.MinSizeBytes() {
		return fmt.Sprintf("The LVMLogicalVolumeConfig %q minimum size shrank; the Machine must be reprovisioned.", name)
	}
	if active.MaxSizePercentVG() != desired.MaxSizePercentVG() {
		return fmt.Sprintf("The LVMLogicalVolumeConfig %q percentage size changed; the Machine must be reprovisioned.", name)
	}
	if active.MaxSizeBytes() != 0 && desired.MaxSizeBytes() != 0 && desired.MaxSizeBytes() < active.MaxSizeBytes() {
		return fmt.Sprintf("The LVMLogicalVolumeConfig %q maximum size shrank; the Machine must be reprovisioned.", name)
	}
	return ""
}

func sameEndpoint(left, right *url.URL) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.String() == right.String()
}

func providerID(provider talosconfig.Provider) (string, bool) {
	kubelet := provider.K8sKubeletConfig()
	if kubelet == nil {
		return "", true
	}
	values := kubelet.ExtraArgs()["provider-id"]
	switch len(values) {
	case 0:
		return "", true
	case 1:
		return values[0], true
	default:
		return "", false
	}
}

func sameKubernetesPKI(active, desired talosconfig.Provider) bool {
	return sameK8sAPIServerCAConfig(active.K8sAPIServerCAConfig(), desired.K8sAPIServerCAConfig()) &&
		sameK8sAggregatorCAConfig(active.K8sAggregatorCAConfig(), desired.K8sAggregatorCAConfig()) &&
		sameK8sServiceAccountConfig(active.K8sServiceAccountConfig(), desired.K8sServiceAccountConfig())
}

func sameK8sAPIServerCAConfig(left, right coreconfig.K8sAPIServerCAConfig) bool {
	if (left == nil) != (right == nil) {
		return false
	}
	if left == nil {
		return true
	}
	if !sameCertificateAndKeyDER(left.IssuingCA(), right.IssuingCA()) {
		return false
	}
	return equalCertificateSet(left.AcceptedCAs(), right.AcceptedCAs())
}

func sameK8sAggregatorCAConfig(left, right coreconfig.K8sAggregatorCAConfig) bool {
	if (left == nil) != (right == nil) {
		return false
	}
	if left == nil {
		return true
	}
	if !sameCertificateAndKeyDER(left.IssuingCA(), right.IssuingCA()) {
		return false
	}
	return equalCertificateSet(left.AcceptedCAs(), right.AcceptedCAs())
}

func sameK8sServiceAccountConfig(left, right coreconfig.K8sServiceAccountConfig) bool {
	if (left == nil) != (right == nil) {
		return false
	}
	if left == nil {
		return true
	}
	if !equalKeySetDER(left.AcceptedKeys(), right.AcceptedKeys()) {
		return false
	}
	if strings.TrimSpace(left.IssuerURL()) != strings.TrimSpace(right.IssuerURL()) {
		return false
	}
	if !equalStringSet(left.AcceptedIssuers(), right.AcceptedIssuers()) {
		return false
	}
	if !equalStringSet(left.APIAudiences(), right.APIAudiences()) {
		return false
	}
	// IssuingKeyはprivate keyであり、identity比較は厳密に行う。AcceptedKeysに含まれるため、同一性はそちらでも担保されるが、念のため直接比較する。
	if !samePEMEncodedKeyDER(left.IssuingKey(), right.IssuingKey()) {
		return false
	}
	return true
}

func sameCertificateAndKeyDER(left, right *siderox509.PEMEncodedCertificateAndKey) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	leftCrtFP, err := certificateFingerprint(left.Crt)
	if err != nil {
		return false
	}
	rightCrtFP, err := certificateFingerprint(right.Crt)
	if err != nil {
		return false
	}
	if leftCrtFP != rightCrtFP {
		return false
	}
	leftKeyFP, err := keyFingerprint(left.Key)
	if err != nil {
		return false
	}
	rightKeyFP, err := keyFingerprint(right.Key)
	if err != nil {
		return false
	}
	return leftKeyFP == rightKeyFP
}

func samePEMEncodedKeyDER(left, right *siderox509.PEMEncodedKey) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	leftFP, err := keyFingerprint(left.Key)
	if err != nil {
		return false
	}
	rightFP, err := keyFingerprint(right.Key)
	if err != nil {
		return false
	}
	return leftFP == rightFP
}

func equalCertificateSet(left, right []*siderox509.PEMEncodedCertificate) bool {
	leftSet, err := certificateSet(left)
	if err != nil {
		return false
	}
	rightSet, err := certificateSet(right)
	if err != nil {
		return false
	}
	if len(leftSet) != len(rightSet) {
		return false
	}
	for fingerprint := range leftSet {
		if _, ok := rightSet[fingerprint]; !ok {
			return false
		}
	}
	return true
}

func equalKeySetDER(left, right []*siderox509.PEMEncodedKey) bool {
	leftSet, err := keySet(left)
	if err != nil {
		return false
	}
	rightSet, err := keySet(right)
	if err != nil {
		return false
	}
	if len(leftSet) != len(rightSet) {
		return false
	}
	for fingerprint := range leftSet {
		if _, ok := rightSet[fingerprint]; !ok {
			return false
		}
	}
	return true
}

func equalStringSet(left, right []string) bool {
	leftSet := stringSet(left)
	rightSet := stringSet(right)
	if len(leftSet) != len(rightSet) {
		return false
	}
	for value := range leftSet {
		if _, ok := rightSet[value]; !ok {
			return false
		}
	}
	return true
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		result[trimmed] = struct{}{}
	}
	return result
}

func certificateFingerprint(data []byte) ([32]byte, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return [32]byte{}, ErrInvalidPKIMaterial
	}
	block, rest := pem.Decode(trimmed)
	if block == nil || block.Type != "CERTIFICATE" {
		return [32]byte{}, fmt.Errorf("%w: PEM block is not a CERTIFICATE", ErrInvalidPKIMaterial)
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		return [32]byte{}, fmt.Errorf("%w: PEM contains trailing data", ErrInvalidPKIMaterial)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return [32]byte{}, fmt.Errorf("%w: parse certificate: %w", ErrInvalidPKIMaterial, err)
	}
	return sha256.Sum256(cert.Raw), nil
}

func keyFingerprint(data []byte) ([32]byte, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return [32]byte{}, ErrInvalidPKIMaterial
	}
	block, rest := pem.Decode(trimmed)
	if block == nil {
		return [32]byte{}, fmt.Errorf("%w: PEM block is not valid", ErrInvalidPKIMaterial)
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		return [32]byte{}, fmt.Errorf("%w: PEM contains trailing data", ErrInvalidPKIMaterial)
	}
	if !strings.Contains(block.Type, "PRIVATE KEY") && !strings.Contains(block.Type, "PUBLIC KEY") {
		return [32]byte{}, fmt.Errorf("%w: unexpected PEM type %q", ErrInvalidPKIMaterial, block.Type)
	}
	hash := sha256.Sum256(block.Bytes)
	return hash, nil
}

func certificateSet(certs []*siderox509.PEMEncodedCertificate) (map[[32]byte]struct{}, error) {
	result := make(map[[32]byte]struct{}, len(certs))
	for _, cert := range certs {
		if cert == nil {
			return nil, ErrInvalidPKIMaterial
		}
		fingerprint, err := certificateFingerprint(cert.Crt)
		if err != nil {
			return nil, err
		}
		result[fingerprint] = struct{}{}
	}
	return result, nil
}

func keySet(keys []*siderox509.PEMEncodedKey) (map[[32]byte]struct{}, error) {
	result := make(map[[32]byte]struct{}, len(keys))
	for _, key := range keys {
		if key == nil {
			return nil, ErrInvalidPKIMaterial
		}
		fingerprint, err := keyFingerprint(key.Key)
		if err != nil {
			return nil, err
		}
		result[fingerprint] = struct{}{}
	}
	return result, nil
}

// normalizeInstallerImageは、比較対象のconfigurationからinstaller image identityの差分を取り除く。
// installer imageの変更はTalos image upgrade pathが所有しており、machine configuration updateの判定へ混入させない。
func normalizeInstallerImage(configuration []byte) ([]byte, error) {
	normalized, err := talos.SetInstallerImage(configuration, normalizedInstallerVersion, normalizedInstallerSchematic)
	if err != nil {
		return nil, fmt.Errorf("normalize installer image: %w", err)
	}
	return normalized, nil
}
