// Package bootstrapはTalos machine configurationの合成とBootstrap Secret contractを扱う。
// immutable Secretの入力検証、Talos machineryへ委譲したpatch合成、complete configurationからのredacted semantic digestとSecret生成を提供し、raw patchをcomplete configurationとして誤配布しない。
// cluster identity、Talos PKI、endpoint、machine role、ProviderID、CAPI version-managed fieldを含む合成とconflict判定は、必要なcontextがcontrollerから渡されるまで未実装のまま安全に停止する。
package bootstrap
