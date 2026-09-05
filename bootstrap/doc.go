// Package bootstrapはTalos machine configurationの合成とBootstrap Secret contractを扱う。
// immutable Secretの入力検証、Talos machineryへ委譲したpatch合成、complete configurationからのredacted semantic digestとSecret生成を提供し、raw patchをcomplete configurationとして誤配布しない。
// cluster identity、Talos PKI、cluster endpoint、machine role、Kubernetes versionをTalos machineryへ渡し、provider-owned invariantを検証した完全configurationを生成する。
package bootstrap
