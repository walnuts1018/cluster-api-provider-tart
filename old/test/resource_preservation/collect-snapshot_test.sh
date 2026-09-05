#!/usr/bin/env bash
set -euo pipefail

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT

mkdir -p "$work_dir/bin"
cp "$script_dir/../fixtures/fake-kubectl" "$work_dir/bin/kubectl"
chmod +x "$work_dir/bin/kubectl"

PATH="$work_dir/bin:$PATH" \
  "$script_dir/collect-snapshot.sh" lifecycle-e2e "$work_dir/snapshot.json"

# API応答からSecretの値を除外し、同一性判定に必要なmetadataだけを保存することを固定する。
jq -e '
  .schemaVersion == "v1alpha1"
  and .cluster.kubernetesVersion == "v1.36.2"
  and (.resources | length) == 3
  and (.resources | any(.kind == "Secret" and .uid == "secret-uid"))
  and (.resources | any(.kind == "PersistentVolume" and .namespace == "" and .uid == "pv-uid"))
  and (.pvcs == [{namespace: "lifecycle-e2e", name: "data", sha256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}])
  and (tostring | contains("secret-value") | not)
' "$work_dir/snapshot.json" >/dev/null

# 採取結果が更新前後比較器の入力契約を満たすことを固定する。
"$script_dir/verify.sh" "$work_dir/snapshot.json" "$work_dir/snapshot.json"
