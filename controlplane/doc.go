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

// Package controlplane will hold the pure policy for TartControlPlane: quorum-safe
// scale up/down decisions, cluster secret bundle generation-management (immutable
// per-generation Secrets, active generation switch only after observing a completed
// Talos CA rotation), initial etcd bootstrap sequencing, and Kubernetes version
// upgrade convergence across Topology-managed and directly managed clusters.
//
// TODO: このpackageの実装は次セッション以降とする。現時点ではcontroller.
// TartControlPlaneReconcilerが観測・Status反映のみを行うskeletonであり、cluster secret
// bundleの生成やTalos Bootstrap RPCの呼び出しはまだ行っていない。実装時はdocs/development/
// lifecycle.mdとapi-contract.mdのCluster secret bundle contractを参照する。
package controlplane
