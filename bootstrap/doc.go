// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package bootstrap will hold the pure policy for synthesizing Talos machine
// configuration for TartBootstrapConfig: combining base configuration, the
// user-owned raw patch read from an immutable Secret, and provider-owned invariants
// (cluster identity, Talos PKI/token, cluster endpoint, machine role, ProviderID,
// CAPI version-managed fields), and rejecting conflicts with
// Ready=False/Reason=ConfigurationConflict instead of silently overwriting them.
//
// TODO: このpackageの実装は次セッション以降とする。現時点ではcontroller.
// TartBootstrapConfigReconcilerが観測・Status反映のみを行うskeletonであり、
// Bootstrap Secretの生成やconfiguration digest計算はまだ呼び出していない。
// 実装時はdocs/development/talos.mdとapi-contract.mdのSecret contractを参照する。
package bootstrap
