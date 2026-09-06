// Package bootstrapはTalos machine configuration合成に関する値オブジェクトと純粋関数を提供する。
// このpackageはsiderolabs machinery型を含む外部依存を一切importしない。base configuration、
// ユーザーraw patch、provider-owned invariantという3つのlayerをどの順序で合成すべきかという意思決定と、
// 完全なconfiguration byte列に対するSHA-256 digest計算だけを扱い、実際のYAML/CEL解釈やmergeは
// adapter/talos/configbuilderへ委譲する。
package bootstrap
