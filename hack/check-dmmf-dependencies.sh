#!/usr/bin/env bash
set -euo pipefail

module=$(go list -m -f '{{.Path}}')
violations=$(
  go list -f '{{.ImportPath}}{{range .Imports}} {{.}}{{end}}' \
    ./internal/domain/... ./internal/application/... |
    awk -v module="$module" '
      {
        package_name = $1
        for (field = 2; field <= NF; field++) {
          dependency = $field
          if (package_name ~ "/internal/domain(/|$)" &&
              dependency ~ ("^" module "/internal/(application|adapter|controller)(/|$)")) {
            print package_name " -> " dependency
          }
          if (package_name ~ "/internal/application(/|$)" &&
              dependency ~ ("^" module "/internal/(adapter|controller)(/|$)")) {
            print package_name " -> " dependency
          }
        }
      }
    '
)

if [[ -n "$violations" ]]; then
  echo "DMMF dependency direction violation:" >&2
  echo "$violations" >&2
  exit 1
fi
