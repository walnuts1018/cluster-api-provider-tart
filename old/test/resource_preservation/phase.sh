#!/usr/bin/env bash
set -euo pipefail

phase=${1:?prepare、before、afterのいずれかを指定してください}
artifact_dir=${2:-_artifacts/cluster-lifecycle}
namespace=lifecycle-e2e
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)

mkdir -p "$artifact_dir"

case "$phase" in
  prepare)
    kubectl apply --server-side --field-manager=cluster-lifecycle-e2e \
      --filename "$repository_root/test/e2e/config/resource-preservation.yaml"
    kubectl rollout status deployment/preservation-web --namespace "$namespace" --timeout=5m
    kubectl rollout status statefulset/preservation-data --namespace "$namespace" --timeout=5m
    # 空ファイルのdigestでは保持確認にならないため、初回採取前にpayload生成を確認する。
    kubectl exec --namespace "$namespace" preservation-data-0 -- test -s /data/payload.bin
    ;;
  before)
    "$script_dir/collect-snapshot.sh" \
      "$namespace" "$artifact_dir/before.json"
    ;;
  after)
    kubectl rollout status deployment/preservation-web --namespace "$namespace" --timeout=5m
    kubectl rollout status statefulset/preservation-data --namespace "$namespace" --timeout=5m
    "$script_dir/collect-snapshot.sh" \
      "$namespace" "$artifact_dir/after.json"
    "$script_dir/verify.sh" \
      "$artifact_dir/before.json" "$artifact_dir/after.json"
    ;;
  *)
    echo "unsupported phase: $phase" >&2
    exit 1
    ;;
esac
