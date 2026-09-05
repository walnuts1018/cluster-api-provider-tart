// Package extensions implements the CAPI Runtime Extension wire protocol for the
// in-place update hooks (CanUpdateMachineSet, CanUpdateMachine, UpdateMachine). The
// real safe-diff decision logic is not implemented yet: every handler returns Failure,
// which CAPI and Tart both treat as a veto, never as permission to fall back to a
// replacement rollout. See .agents/skills/reconcile/SKILL.md#runtime-extension.
package extensions

import (
	"fmt"

	runtimecatalog "sigs.k8s.io/cluster-api/api/runtime/catalog"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
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
