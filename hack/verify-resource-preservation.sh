#!/usr/bin/env bash
set -euo pipefail

# 更新前後のUIDとPVC payload digestを比較し、再作成やデータ破壊を検出する。
before=${1:?更新前スナップショットのJSONを指定してください}
after=${2:?更新後スナップショットのJSONを指定してください}
command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }

jq -S '.resources | map({key: (.namespace + "/" + .kind + "/" + .name), uid: .uid}) | from_entries' "$before" >"${before}.uids"
jq -S '.resources | map({key: (.namespace + "/" + .kind + "/" + .name), uid: .uid}) | from_entries' "$after" >"${after}.uids"
diff -u "${before}.uids" "${after}.uids" || { echo "resource UID changed" >&2; exit 1; }

jq -S '.pvcs | map({key: (.namespace + "/" + .name), sha256}) | from_entries' "$before" >"${before}.pvcs"
jq -S '.pvcs | map({key: (.namespace + "/" + .name), sha256}) | from_entries' "$after" >"${after}.pvcs"
diff -u "${before}.pvcs" "${after}.pvcs" || { echo "PVC payload digest changed" >&2; exit 1; }
