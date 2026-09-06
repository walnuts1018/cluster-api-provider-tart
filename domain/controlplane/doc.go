// Package controlplaneはetcd quorum維持、証明書bundleの世代管理、CA rotation進行判定に関する値オブジェクトと純粋関数を提供する。
// このpackageはTalos machinery型やKubernetes clientを含む外部依存を一切importせず、観測値だけから安全判定を行う。
// 実際のbundle生成やTalos configuration読み取りはadapter層へ委譲する。
package controlplane
