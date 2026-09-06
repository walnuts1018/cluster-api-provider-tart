// Package extensionsはCAPI Runtime Extensionのwire protocolとTalos image in-place update hookを実装する。安全に完全評価できない差分はpatchなしで拒否し、実行時には現行Host bindingとimmutable Bootstrap Secretを再観測してからTalos APIへ委譲する。
package extensions

import (
	"fmt"

	runtimecatalog "sigs.k8s.io/cluster-api/api/runtime/catalog"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
)

// NewCatalogはin-place update hookを登録したRuntime Extension catalogを生成する。
func NewCatalog() (*runtimecatalog.Catalog, error) {
	catalog := runtimecatalog.New()
	if err := runtimehooksv1.AddToCatalog(catalog); err != nil {
		return nil, fmt.Errorf("add hooks to runtime extension catalog: %w", err)
	}
	return catalog, nil
}
