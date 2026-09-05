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

// Package bootstrapはTalos machine configurationの合成とBootstrap Secret contractを扱う。
// immutable Secretの入力検証とcomplete configurationからのSecret生成を提供し、raw patchを
// complete configurationとして誤配布しない。cluster identity、Talos PKI、endpoint、machine
// role、ProviderID、CAPI version-managed fieldを含む合成とconflict判定は、必要なcontextが
// controllerから渡されるまで未実装のまま安全に停止する。
package bootstrap
