#!/usr/bin/env bash
set -euo pipefail

module=$(go list -m -f '{{.Path}}')

go run ./test/architecture/check_workflow_contracts.go

for workflow_root in domain/*/workflow; do
  [[ -d "$workflow_root" ]] || continue
  [[ "$workflow_root" == "domain/shared/workflow" ]] && continue
  if find "$workflow_root" -maxdepth 1 -type f -name '*.go' | grep -q .; then
    echo "Workflow root must not contain Go files: $workflow_root" >&2
    exit 1
  fi

  for workflow_package in "$workflow_root"/*; do
    [[ -d "$workflow_package" ]] || continue
    workflow_count=$(rg -l '^type Workflow struct' "$workflow_package" --glob '*.go' | wc -l | tr -d ' ')
    if [[ "$workflow_count" != "1" ]]; then
      echo "Workflow package must contain exactly one Workflow struct: $workflow_package" >&2
      exit 1
    fi

    exported_methods=$(rg '^func \([^)]*\*?Workflow\) [A-Z][A-Za-z0-9_]*\(' "$workflow_package" --glob '*.go' --glob '!**/*_test.go' || true)
    if [[ $(printf '%s\n' "$exported_methods" | sed '/^$/d' | wc -l | tr -d ' ') != "1" ]] ||
      [[ "$exported_methods" != *") Do("* ]]; then
      echo "Workflow package must expose only Workflow.Do: $workflow_package" >&2
      exit 1
    fi
  done
done

if find domain -type d -name deps | grep -q .; then
  echo "Workflow dependencies must be declared in each workflow package; domain/**/deps is not allowed" >&2
  exit 1
fi

if rg -n 'type .*Step interface|NewWorkflowWithSteps|type Executor struct' domain --glob '*.go'; then
  echo "Step must be a directly-called quasi-pure function, not an injected interface or Executor" >&2
  exit 1
fi

for context in domain/*; do
  [[ -d "$context" ]] || continue
  [[ "$context" == "domain/shared" ]] && continue
  if [[ ! -d "$context/workflow" ]]; then
    echo "Bounded context must own at least one workflow: $context" >&2
    exit 1
  fi
done

violations=$(
  go list -f '{{.ImportPath}}{{range .Imports}} {{.}}{{end}}' \
    ./domain/... |
    awk -v module="$module" '
      {
        package_name = $1
        for (field = 2; field <= NF; field++) {
          dependency = $field
          if ((package_name ~ "/domain/[^/]+/entity(/|$)" ||
               package_name ~ "/domain/shared(/|$)") &&
              dependency ~ ("^" module "/(api|infrastructure)(/|$)")) {
            print package_name " -> " dependency
          }
          if (package_name ~ "/domain/[^/]+/(workflow|step)(/|$)" &&
              dependency ~ ("^" module "/infrastructure(/|$)")) {
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
