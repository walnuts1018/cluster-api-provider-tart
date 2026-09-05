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

// Package controlplaneはTartControlPlaneのquorum安全性、cluster secret bundle世代管理、
// etcd bootstrap、Kubernetes version収束の純粋なpolicyを扱う。bundleのmaterial生成とTalos
// operationはTalos machineryへ委譲し、このpackageはimmutable Secretの境界と世代遷移を検証する。
package controlplane
