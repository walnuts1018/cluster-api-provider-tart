#!/usr/bin/env bash
# run.shは、clusterctl/cluster-api-operatorがGitHub Releaseから取得する
# infrastructure-components.yamlとmetadata.yamlを組み立てる。config/manager、
# config/netboot-serverのimageをRELEASE用のtagへ差し替えてからconfig/defaultをbuildする。
set -euo pipefail

: "${CONTROLLER_IMAGE:?CONTROLLER_IMAGE is required (e.g. ghcr.io/OWNER/cluster-api-provider-tart:vX.Y.Z)}"
: "${NETBOOT_SERVER_IMAGE:?NETBOOT_SERVER_IMAGE is required (e.g. ghcr.io/OWNER/cluster-api-provider-tart-netboot-server:vX.Y.Z)}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT_DIR="${1:-${REPO_ROOT}/out}"
mkdir -p "${OUT_DIR}"

# kustomization.yamlをimage差し替え前の状態へ確実に戻せるよう、gitのtrack有無に関わらずcopyでbackupする。
MANAGER_KUSTOMIZATION="${REPO_ROOT}/config/manager/kustomization.yaml"
NETBOOT_KUSTOMIZATION="${REPO_ROOT}/config/netboot-server/kustomization.yaml"
MANAGER_KUSTOMIZATION_BACKUP="$(mktemp)"
NETBOOT_KUSTOMIZATION_BACKUP="$(mktemp)"
cp "${MANAGER_KUSTOMIZATION}" "${MANAGER_KUSTOMIZATION_BACKUP}"
cp "${NETBOOT_KUSTOMIZATION}" "${NETBOOT_KUSTOMIZATION_BACKUP}"
restore_kustomizations() {
  cp "${MANAGER_KUSTOMIZATION_BACKUP}" "${MANAGER_KUSTOMIZATION}"
  cp "${NETBOOT_KUSTOMIZATION_BACKUP}" "${NETBOOT_KUSTOMIZATION}"
  rm -f "${MANAGER_KUSTOMIZATION_BACKUP}" "${NETBOOT_KUSTOMIZATION_BACKUP}"
}
trap restore_kustomizations EXIT

cd "${REPO_ROOT}/config/manager"
kustomize edit set image "controller=${CONTROLLER_IMAGE}"

cd "${REPO_ROOT}/config/netboot-server"
kustomize edit set image "netboot-server=${NETBOOT_SERVER_IMAGE}"

cd "${REPO_ROOT}"
kustomize build config/default > "${OUT_DIR}/infrastructure-components.yaml"
cp metadata.yaml "${OUT_DIR}/metadata.yaml"

# config/operator/infrastructure-provider.yamlはcluster-api-operatorのInfrastructureProvider CRの
# サンプルであり、versionのプレースホルダーを実際のrelease tagへ置換してassetとして同梱する。
sed "s#REPLACE_WITH_RELEASE_VERSION#${RELEASE_TAG:-latest}#" \
  "${REPO_ROOT}/config/operator/infrastructure-provider.yaml" > "${OUT_DIR}/infrastructure-provider.yaml"
