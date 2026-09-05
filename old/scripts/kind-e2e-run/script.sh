#!/usr/bin/env bash
# Kind clusterをテスト結果にかかわらず削除し、次回実行へ状態を持ち越さない。
set -euo pipefail

mise run setup-test-e2e
trap 'mise run cleanup-test-e2e' EXIT

kind_cluster="${KIND_CLUSTER:-cluster-api-provider-tart-test-e2e}"
KIND=kind KIND_CLUSTER="${kind_cluster}" go test -tags=e2e ./test/e2e/ -v -ginkgo.v
