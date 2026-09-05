#!/usr/bin/env bash
set -euo pipefail

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT

write_snapshot() {
  local path=$1
  local deployment_uid=$2
  local pvc_digest=$3

  jq -n \
    --arg deployment_uid "$deployment_uid" \
    --arg pvc_digest "$pvc_digest" \
    '{
      resources: [
        {
          apiVersion: "apps/v1",
          kind: "Deployment",
          namespace: "lifecycle-e2e",
          name: "identity",
          uid: $deployment_uid
        }
      ],
      pvcs: [
        {
          namespace: "lifecycle-e2e",
          name: "data",
          sha256: $pvc_digest
        }
      ]
    }' >"$path"
}

expect_failure() {
  local name=$1
  local before=$2
  local after=$3

  if "$script_dir/verify.sh" "$before" "$after" >/dev/null 2>&1; then
    echo "$name: expected verification to fail" >&2
    exit 1
  fi
}

digest_a=$(printf 'a%.0s' {1..64})
digest_b=$(printf 'b%.0s' {1..64})
write_snapshot "$work_dir/before.json" "deployment-uid" "$digest_a"
write_snapshot "$work_dir/same.json" "deployment-uid" "$digest_a"

# 同一スナップショットを受理できることを固定する。
"$script_dir/verify.sh" "$work_dir/before.json" "$work_dir/same.json"

# Resourceが再作成された場合にUID差分を検出することを固定する。
write_snapshot "$work_dir/changed-uid.json" "replacement-uid" "$digest_a"
expect_failure "resource UID change" "$work_dir/before.json" "$work_dir/changed-uid.json"

# PVC内容が変化した場合にdigest差分を検出することを固定する。
write_snapshot "$work_dir/changed-pvc.json" "deployment-uid" "$digest_b"
expect_failure "PVC digest change" "$work_dir/before.json" "$work_dir/changed-pvc.json"

# 欠損値をnullとして比較しないよう、不正なスキーマを拒否することを固定する。
jq 'del(.resources[0].uid)' "$work_dir/before.json" >"$work_dir/missing-uid.json"
expect_failure "missing UID" "$work_dir/missing-uid.json" "$work_dir/same.json"
