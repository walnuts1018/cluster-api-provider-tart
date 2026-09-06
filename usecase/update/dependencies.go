package update

import (
	domainupdate "github.com/walnuts1018/cluster-api-provider-tart/domain/update"
)

// ConfigDiffClassifierは、active configurationとdesired configurationの意味的な差分を分類する
// interfaceである。実装はsiderolabs machinery型への変換・比較を自身に閉じ込め、このpackageは
// domain/updateが定義するChangeClassとreasonだけを受け取る。
type ConfigDiffClassifier interface {
	ClassifyConfigurationChange(active, desired []byte) (class domainupdate.ChangeClass, reason string, err error)
}
