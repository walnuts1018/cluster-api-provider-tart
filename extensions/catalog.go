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

// Package extensions implements the CAPI Runtime Extension wire protocol for the
// in-place update hooks (CanUpdateMachineSet, CanUpdateMachine, UpdateMachine). The
// real safe-diff decision logic is not implemented yet: every handler returns Failure,
// which CAPI and Tart both treat as a veto, never as permission to fall back to a
// replacement rollout. See .agents/skills/reconcile/SKILL.md#runtime-extension.
package extensions

import (
	"fmt"

	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	runtimecatalog "sigs.k8s.io/cluster-api/exp/runtime/catalog"
)

// NewCatalog creates a Runtime Extension catalog with the in-place update hooks
// registered.
func NewCatalog() (*runtimecatalog.Catalog, error) {
	catalog := runtimecatalog.New()
	if err := runtimehooksv1.AddToCatalog(catalog); err != nil {
		return nil, fmt.Errorf("add hooks to runtime extension catalog: %w", err)
	}
	return catalog, nil
}
