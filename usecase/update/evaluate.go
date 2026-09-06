// Package updateは、稼働中Talos Nodeへのmachine configuration適用可否を判定するusecaseを提供する。
// 差分の意味的分類はConfigDiffClassifier interface(実装はadapter/talos/configbuilder)へ委譲し、
// 分類結果からのapply mode決定はdomain/updateの純粋関数へ委譲する。このpackage自体は
// 両者をオーケストレーションするだけで、siderolabs machinery型を直接保持しない。
package update

import (
	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	domainupdate "github.com/walnuts1018/cluster-api-provider-tart/domain/update"
)

// Evaluateはactive configurationとdesired configurationの差分をapply strategyと組み合わせて判定する。
// 判定はrebootの要否ではなく「data、identityを破壊するか」というsafety boundaryで行う。
func Evaluate(classifier ConfigDiffClassifier, strategy bootstrapv1alpha1.ConfigurationApplyStrategy, active, desired []byte) (domainupdate.Decision, error) {
	class, reason, err := classifier.ClassifyConfigurationChange(active, desired)
	if err != nil {
		return domainupdate.Decision{}, err
	}
	return domainupdate.Decide(class, reason, strategy)
}
