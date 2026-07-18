#!/usr/bin/env bash
set -euo pipefail

before=${1:?更新前スナップショットのJSONを指定してください}
after=${2:?更新後スナップショットのJSONを指定してください}

command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }
[[ -r "$before" ]] || { echo "before snapshot is not readable: $before" >&2; exit 1; }
[[ -r "$after" ]] || { echo "after snapshot is not readable: $after" >&2; exit 1; }

work_dir=$(mktemp -d)
# 比較用の正規化データにはUIDやdigestが含まれるため、成功時も必ず削除する。
trap 'rm -rf "$work_dir"' EXIT

normalize_snapshot() {
  local source=$1
  local destination=$2

  jq -eS '
    def required_string($name):
      if type == "string" and length > 0 then . else error($name + " must be a non-empty string") end;
    def string_value($name):
      if type == "string" then . else error($name + " must be a string") end;
    def insert_unique($key; $value):
      if has($key) then error("duplicate snapshot key: " + $key) else .[$key] = $value end;

    if type != "object" then error("snapshot must be an object") else . end
    | if (.resources | type) != "array" then error("resources must be an array") else . end
    | if (.pvcs | type) != "array" then error("pvcs must be an array") else . end
    | {
        resources: reduce .resources[] as $resource ({};
          ($resource.namespace | string_value("resource namespace")) as $namespace
          | ($resource.kind | required_string("resource kind")) as $kind
          | ($resource.name | required_string("resource name")) as $name
          | ($resource.uid | required_string("resource uid")) as $uid
          | insert_unique($namespace + "/" + $kind + "/" + $name; $uid)
        ),
        pvcs: reduce .pvcs[] as $pvc ({};
          ($pvc.namespace | required_string("PVC namespace")) as $namespace
          | ($pvc.name | required_string("PVC name")) as $name
          | ($pvc.sha256 | required_string("PVC sha256")) as $sha256
          | if ($sha256 | test("^[0-9a-f]{64}$"))
            then insert_unique($namespace + "/" + $name; $sha256)
            else error("PVC sha256 must be 64 lowercase hexadecimal characters")
            end
        )
      }
  ' "$source" >"$destination"
}

normalize_snapshot "$before" "$work_dir/before.json"
normalize_snapshot "$after" "$work_dir/after.json"

if ! diff -u "$work_dir/before.json" "$work_dir/after.json"; then
  echo "Kubernetes resource UID or PVC payload digest changed" >&2
  exit 1
fi
