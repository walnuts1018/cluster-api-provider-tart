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

// Package ocireference は oci:// スキームを持つ OCI image reference を表す。
package ocireference

import (
	"fmt"
	"strings"

	"oras.land/oras-go/v2/registry"
)

// Parse は OCI image reference を検証し、解析済みの registry reference を返す。
func Parse(value string) (registry.Reference, error) {
	if !strings.HasPrefix(value, "oci://") {
		return registry.Reference{}, fmt.Errorf("OCI image reference must use oci://")
	}
	ref, err := registry.ParseReference(strings.TrimPrefix(value, "oci://"))
	if err != nil {
		return registry.Reference{}, fmt.Errorf("parse OCI image reference: %w", err)
	}
	return ref, nil
}
