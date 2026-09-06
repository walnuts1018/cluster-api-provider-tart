// Package recoveryはTalos cluster identityごとのrecovery Secret契約と、Retained Hostに対する破壊的操作の前提となるidentity検証のオーケストレーションを提供する。
// Machineの寿命とHost上のTalos installationの寿命は一致しないため、recovery identityはTartMachineやTartBootstrapConfigのownership lifecycleから独立した寿命を持つ。
// 詳細は.agents/skills/host-lifecycle/SKILL.mdとdocs/development/lifecycle.mdを参照する。
package recovery

import (
	"strings"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	domainrecovery "github.com/walnuts1018/cluster-api-provider-tart/domain/recovery"
)

// ExpectedIdentityForHostはTartHostとrecovery identityからReset前に照合すべき期待値を組み立てる。
func ExpectedIdentityForHost(hostObject *infrav1alpha1.TartHost, clusterID, endpoint string) domainrecovery.ExpectedIdentity {
	expected := domainrecovery.ExpectedIdentity{
		ClusterID: strings.TrimSpace(clusterID),
		Endpoint:  strings.TrimSpace(endpoint),
	}
	if hostObject == nil {
		return expected
	}
	expected.MACAddress = hostObject.Spec.MACAddress
	if hostObject.Status.Inventory != nil {
		expected.SystemUUID = strings.TrimSpace(hostObject.Status.Inventory.SystemUUID)
	}
	return expected
}
