package extension

import (
	"fmt"

	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	runtimecatalog "sigs.k8s.io/cluster-api/exp/runtime/catalog"
)

// NewCatalog creates a new runtime catalog with all in-place update hooks registered.
func NewCatalog() (*runtimecatalog.Catalog, error) {
	catalog := runtimecatalog.New()
	if err := runtimehooksv1.AddToCatalog(catalog); err != nil {
		return nil, fmt.Errorf("failed to add hooks to catalog: %w", err)
	}
	return catalog, nil
}
