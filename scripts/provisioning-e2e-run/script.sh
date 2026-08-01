#!/usr/bin/env bash
# ローカルのProvisioning E2Eを実行する。
set -euo pipefail
IMG="${IMG:-registry.test.walnuts.dev/cluster-api-provider-tart:e2e}"
CONTAINER_TOOL="${CONTAINER_TOOL:-docker}"
mise run setup-test-provisioning-k3s
mise run build-installer-e2e
set -a
# 生成時に作る環境ファイルであり、静的解析時には存在しない。
# shellcheck source=/dev/null
. "${PROVISIONING_E2E_OUTPUT_DIR:-dist/provisioning-e2e}/env"
set +a
"$CONTAINER_TOOL" build -t "$IMG" .
# k3sがローカルにある場合は、ビルド済みimageを明示的に取り込む。
if command -v k3s &> /dev/null; then
  "$CONTAINER_TOOL" save "$IMG" | sudo k3s ctr images import -
fi

ARTIFACTS="${ARTIFACTS:-${PWD}/_artifacts}"
mkdir -p "$ARTIFACTS"
export KUBECONFIG=~/.kube/config
# clusterctlは設定内の${PWD}を展開しないため、実行前に展開する。
CONFIG_TEMP=$(mktemp)
trap 'rm -f "$CONFIG_TEMP"' EXIT
export PWD="${PWD}"
envsubst < "${PWD}/test/e2e/config/tart.yaml" > "$CONFIG_TEMP"
set -- go test -tags=e2e ./test/e2e/provisioning/... -timeout 30m -v -ginkgo.v -e2e.artifacts-folder="$ARTIFACTS" -e2e.config="$CONFIG_TEMP" -e2e.use-existing-cluster=true
if [ -n "${PROVISIONING_E2E_GINKGO_FOCUS:-}" ]; then
  set -- "$@" "-ginkgo.focus=${PROVISIONING_E2E_GINKGO_FOCUS}"
fi
"$@"
