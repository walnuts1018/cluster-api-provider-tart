package update

import (
	"bytes"
	"fmt"
	"reflect"

	"net/url"

	"github.com/siderolabs/crypto/x509"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos/configbuilder"
	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/talos"
)

// normalizedInstallerImageは、installer image identityを比較対象から外すためのsentinel値である。
const (
	normalizedInstallerVersion   = "v0.0.0"
	normalizedInstallerSchematic = "normalized"
)

// ChangeClassはactive configurationとdesired configurationの差分の分類である。
type ChangeClass string

const (
	// ChangeNoneは意味的な差分がないことを表す。
	ChangeNone ChangeClass = "None"
	// ChangeUpdatableはdata、identityを破壊しないため、policyに従ってin-placeで適用できる差分を表す。
	ChangeUpdatable ChangeClass = "Updatable"
	// ChangeReprovisionRequiredはdataまたはidentityを破壊するため、通常のupdateとして適用できない差分を表す。
	ChangeReprovisionRequired ChangeClass = "ReprovisionRequired"
	// ChangeInvariantConflictはprovider-owned invariantと競合する差分を表す。
	ChangeInvariantConflict ChangeClass = "InvariantConflict"
)

// Decisionはconfiguration差分に対する判定結果である。Reasonは利用者可視のためのメッセージとして英語で保持する。
type Decision struct {
	Class     ChangeClass
	ApplyMode ApplyMode
	Reason    string
}

// updatableDocumentKindsは、on-diskのdataやnode identityを破壊せずに反映できるconfiguration documentのkindである。
// ここに列挙されていないkind(volume、LVM、RAID、swap、unattended install、discovery identityなど、および将来追加される
// 未知のkind)は判定できないものとして安全側(ReprovisionRequired)へ倒す。
var updatableDocumentKinds = map[string]struct{}{
	// network
	"BGPInstanceConfig":       {},
	"BlackholeRouteConfig":    {},
	"BondConfig":              {},
	"BridgeConfig":            {},
	"DummyLinkConfig":         {},
	"EthernetConfig":          {},
	"HCloudVIPConfig":         {},
	"HostnameConfig":          {},
	"HTTPProbeConfig":         {},
	"KubeSpanConfig":          {},
	"KubeSpanEndpointsConfig": {},
	"LinkAliasConfig":         {},
	"LinkConfig":              {},
	"NetworkRuleConfig":       {},
	"ResolverConfig":          {},
	"RoutingRuleConfig":       {},
	"SideroLinkConfig":        {},
	"StaticHostConfig":        {},
	"TCPProbeConfig":          {},
	"TimeSyncConfig":          {},
	"VethConfig":              {},
	"VLANConfig":              {},
	"VRFConfig":               {},
	"WireguardConfig":         {},
	// runtime、Kubernetes
	"ContainerConfig":          {},
	"CRIBaseRuntimeSpecConfig": {},
	"CRICustomizationConfig":   {},
	"DiscoveryServiceConfig":   {},
	"EnvironmentConfig":        {},
	"EtcFileConfig":            {},
	"EventSinkConfig":          {},
	"ExtensionServiceConfig":   {},
	"FilesystemScrubConfig":    {},
	"FilesystemTrimConfig":     {},
	"ImageVerificationConfig":  {},
	"KernelModuleConfig":       {},
	"KmsgLogConfig":            {},
	"KubeletConfig":            {},
	"OOMConfig":                {},
	"SecurityProfileConfig":    {},
	"SysctlConfig":             {},
	"SysfsConfig":              {},
	"TrustedRootsConfig":       {},
	"UdevRulesConfig":          {},
	"WatchdogTimerConfig":      {},
}

// v1alpha1DocumentKindは従来のmonolithic machine configuration document(legacy document)のkindである。
const v1alpha1DocumentKind = "v1alpha1"

// Evaluateはactive configurationとdesired configurationの差分をpolicyと組み合わせて判定する。
// 判定はrebootの要否ではなく「data、identityを破壊するか」というsafety boundaryで行う。
func Evaluate(policy bootstrapv1alpha1.ConfigurationUpdatePolicy, active, desired []byte) (Decision, error) {
	class, reason, err := ClassifyConfigurationChange(active, desired)
	if err != nil {
		return Decision{}, err
	}
	switch class {
	case ChangeNone:
		return Decision{Class: ChangeNone}, nil
	case ChangeInvariantConflict, ChangeReprovisionRequired:
		return Decision{Class: class, Reason: reason}, nil
	case ChangeUpdatable:
		// policyに従って適用modeを決めるため、この関数の後半で扱う。
	}
	if policy == bootstrapv1alpha1.ConfigurationUpdatePolicyInitialOnly {
		return Decision{
			Class:  ChangeReprovisionRequired,
			Reason: "The machine configuration changed while the update policy is InitialOnly; the Machine is stopped for review.",
		}, nil
	}
	mode, err := ResolveApplyMode(policy)
	if err != nil {
		return Decision{}, err
	}
	return Decision{Class: ChangeUpdatable, ApplyMode: mode, Reason: reason}, nil
}

// ClassifyConfigurationChangeは2つのcomplete machine configurationの差分を分類する。
// configurationを読み込めない場合は判定できないためerrorを返し、呼び出し側はfail-closedで停止する。
func ClassifyConfigurationChange(active, desired []byte) (ChangeClass, string, error) {
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
	activeDigest, err := configbuilder.DigestEffectiveConfiguration(active)
	if err != nil {
		return "", "", fmt.Errorf("digest active machine configuration: %w", err)
	}
	desiredDigest, err := configbuilder.DigestEffectiveConfiguration(desired)
	if err != nil {
		return "", "", fmt.Errorf("digest desired machine configuration: %w", err)
	}
	if activeDigest == desiredDigest {
		return ChangeNone, "", nil
	}
	if reason := invariantConflict(activeProvider, desiredProvider); reason != "" {
		return ChangeInvariantConflict, reason, nil
	}
	if reason := destructiveChange(activeProvider, desiredProvider); reason != "" {
		return ChangeReprovisionRequired, reason, nil
	}
	return ChangeUpdatable, "The machine configuration difference preserves node data and identity.", nil
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
	if !reflect.DeepEqual(active.K8sAPIServerCAConfig(), desired.K8sAPIServerCAConfig()) || !reflect.DeepEqual(active.K8sAggregatorCAConfig(), desired.K8sAggregatorCAConfig()) || !reflect.DeepEqual(active.K8sServiceAccountConfig(), desired.K8sServiceAccountConfig()) {
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
	if providerID(active) != providerID(desired) {
		return "The kubelet ProviderID changed; the update is stopped."
	}
	return ""
}

// destructiveChangeは、disk layout、installation target、既存volumeのwipe、node identityを破壊する差分を検出する。
// 判定できないconfiguration document kindも安全側としてここで停止させる。
func destructiveChange(active, desired talosconfig.Provider) string {
	activeInstall, desiredInstall := active.Machine().Install(), desired.Machine().Install()
	if (activeInstall == nil) != (desiredInstall == nil) {
		return "The install configuration was added or removed; the Machine must be reprovisioned."
	}
	if activeInstall != nil && desiredInstall != nil {
		if activeInstall.Disk() != desiredInstall.Disk() || !sameDiskMatchExpression(activeInstall, desiredInstall) {
			return "The install disk selection changed; the Machine must be reprovisioned."
		}
		if activeInstall.Zero() != desiredInstall.Zero() {
			return "The install wipe setting changed; the Machine must be reprovisioned."
		}
	}
	activeDocuments := documentDigests(active)
	desiredDocuments := documentDigests(desired)
	for key, activeValue := range activeDocuments {
		desiredValue, exists := desiredDocuments[key]
		if !exists || activeValue != desiredValue {
			if reason := documentChangeReason(key); reason != "" {
				return reason
			}
		}
	}
	for key := range desiredDocuments {
		if _, exists := activeDocuments[key]; exists {
			continue
		}
		if reason := documentChangeReason(key); reason != "" {
			return reason
		}
	}
	return ""
}

type documentKey struct {
	apiVersion string
	kind       string
	name       string
}

func documentChangeReason(key documentKey) string {
	if key.kind == v1alpha1DocumentKind {
		// v1alpha1 Config内部の差分はinstall、identity、PKIとして個別に判定済みである。
		return ""
	}
	if _, updatable := updatableDocumentKinds[key.kind]; updatable {
		return ""
	}
	return fmt.Sprintf("The %s configuration document changed and cannot be proven to preserve node data or identity; the Machine must be reprovisioned.", key.kind)
}

func documentDigests(provider talosconfig.Provider) map[documentKey]string {
	digests := make(map[documentKey]string)
	for _, document := range provider.Documents() {
		key := documentKey{apiVersion: document.APIVersion(), kind: document.Kind()}
		if named, ok := document.(config.NamedDocument); ok {
			key.name = named.Name()
		}
		encoded, err := encoder.NewEncoder(document, encoder.WithComments(encoder.CommentsDisabled)).Encode()
		if err != nil {
			// encodeできないdocumentは内容を比較できないため、常に差分ありとして扱い安全側へ倒す。
			digests[key] = fmt.Sprintf("unencodable: %v", err)
			continue
		}
		digests[key] = string(encoded)
	}
	return digests
}

func sameDiskMatchExpression(active, desired config.Install) bool {
	activeExpression, activeErr := active.DiskMatchExpression()
	desiredExpression, desiredErr := desired.DiskMatchExpression()
	if activeErr != nil || desiredErr != nil {
		// 評価できないdisk selectorは同一と証明できないため、差分ありとして扱う。
		return false
	}
	if activeExpression == nil || desiredExpression == nil {
		return activeExpression == nil && desiredExpression == nil
	}
	return activeExpression.String() == desiredExpression.String()
}

func sameCertificateAndKey(left, right *x509.PEMEncodedCertificateAndKey) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return bytes.Equal(left.Crt, right.Crt) && bytes.Equal(left.Key, right.Key)
}

func sameEndpoint(left, right *url.URL) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.String() == right.String()
}

func componentImages(provider talosconfig.Provider) [5]string {
	var images [5]string
	if config := provider.K8sAPIServerConfig(); config != nil {
		images[0] = config.Image()
	}
	if config := provider.K8sControllerManagerConfig(); config != nil {
		images[1] = config.Image()
	}
	if config := provider.K8sSchedulerConfig(); config != nil {
		images[2] = config.Image()
	}
	if config := provider.K8sProxyConfig(); config != nil {
		images[3] = config.Image()
	}
	if config := provider.K8sKubeletConfig(); config != nil {
		images[4] = config.Image()
	}
	return images
}

func providerID(provider talosconfig.Provider) string {
	kubelet := provider.K8sKubeletConfig()
	if kubelet == nil {
		return ""
	}
	values := kubelet.ExtraArgs()["provider-id"]
	if len(values) == 0 {
		return ""
	}
	return values[0]
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
