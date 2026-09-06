#!/usr/bin/env bash
# run.shは、clusterctl/cluster-api-operatorがGitHub Releaseから取得する
# bootstrap/control-plane/infrastructure-components.yamlとmetadata.yamlを組み立てる。
# config/manager、config/netboot-serverのimageをRELEASE用のtagへ差し替えてから、
# config/default配下の3つのprovider別overlayをそれぞれbuildする。
set -euo pipefail

: "${BOOTSTRAP_MANAGER_IMAGE:?BOOTSTRAP_MANAGER_IMAGE is required (e.g. ghcr.io/OWNER/cluster-api-provider-tart/bootstrap-manager:vX.Y.Z)}"
: "${CONTROL_PLANE_MANAGER_IMAGE:?CONTROL_PLANE_MANAGER_IMAGE is required (e.g. ghcr.io/OWNER/cluster-api-provider-tart/control-plane-manager:vX.Y.Z)}"
: "${INFRASTRUCTURE_MANAGER_IMAGE:?INFRASTRUCTURE_MANAGER_IMAGE is required (e.g. ghcr.io/OWNER/cluster-api-provider-tart/infrastructure-manager:vX.Y.Z)}"
: "${NETBOOT_SERVER_IMAGE:?NETBOOT_SERVER_IMAGE is required (e.g. ghcr.io/OWNER/cluster-api-provider-tart/netboot-server:vX.Y.Z)}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT_DIR="${1:-${REPO_ROOT}/out}"
mkdir -p "${OUT_DIR}"

# kustomization.yamlをimage差し替え前の状態へ確実に戻せるよう、gitのtrack有無に関わらずcopyでbackupする。
BOOTSTRAP_KUSTOMIZATION="${REPO_ROOT}/config/manager/bootstrap/kustomization.yaml"
CONTROL_PLANE_KUSTOMIZATION="${REPO_ROOT}/config/manager/control-plane/kustomization.yaml"
INFRASTRUCTURE_KUSTOMIZATION="${REPO_ROOT}/config/manager/infrastructure/kustomization.yaml"
NETBOOT_KUSTOMIZATION="${REPO_ROOT}/config/netboot-server/kustomization.yaml"

BOOTSTRAP_KUSTOMIZATION_BACKUP="$(mktemp)"
CONTROL_PLANE_KUSTOMIZATION_BACKUP="$(mktemp)"
INFRASTRUCTURE_KUSTOMIZATION_BACKUP="$(mktemp)"
NETBOOT_KUSTOMIZATION_BACKUP="$(mktemp)"

cp "${BOOTSTRAP_KUSTOMIZATION}" "${BOOTSTRAP_KUSTOMIZATION_BACKUP}"
cp "${CONTROL_PLANE_KUSTOMIZATION}" "${CONTROL_PLANE_KUSTOMIZATION_BACKUP}"
cp "${INFRASTRUCTURE_KUSTOMIZATION}" "${INFRASTRUCTURE_KUSTOMIZATION_BACKUP}"
cp "${NETBOOT_KUSTOMIZATION}" "${NETBOOT_KUSTOMIZATION_BACKUP}"

restore_kustomizations() {
  cp "${BOOTSTRAP_KUSTOMIZATION_BACKUP}" "${BOOTSTRAP_KUSTOMIZATION}"
  cp "${CONTROL_PLANE_KUSTOMIZATION_BACKUP}" "${CONTROL_PLANE_KUSTOMIZATION}"
  cp "${INFRASTRUCTURE_KUSTOMIZATION_BACKUP}" "${INFRASTRUCTURE_KUSTOMIZATION}"
  cp "${NETBOOT_KUSTOMIZATION_BACKUP}" "${NETBOOT_KUSTOMIZATION}"
  rm -f \
    "${BOOTSTRAP_KUSTOMIZATION_BACKUP}" \
    "${CONTROL_PLANE_KUSTOMIZATION_BACKUP}" \
    "${INFRASTRUCTURE_KUSTOMIZATION_BACKUP}" \
    "${NETBOOT_KUSTOMIZATION_BACKUP}"
}
trap restore_kustomizations EXIT

cd "${REPO_ROOT}/config/manager/bootstrap"
kustomize edit set image "bootstrap-controller=${BOOTSTRAP_MANAGER_IMAGE}"

cd "${REPO_ROOT}/config/manager/control-plane"
kustomize edit set image "control-plane-controller=${CONTROL_PLANE_MANAGER_IMAGE}"

cd "${REPO_ROOT}/config/manager/infrastructure"
kustomize edit set image "infrastructure-controller=${INFRASTRUCTURE_MANAGER_IMAGE}"

cd "${REPO_ROOT}/config/netboot-server"
kustomize edit set image "netboot-server=${NETBOOT_SERVER_IMAGE}"

cd "${REPO_ROOT}"
# config/crd/bases、config/default/manager_metrics_patch.yamlをprovider別overlayの外側から
# 参照しているため、kustomizeのdefaultのpath traversal制限を緩和する必要がある。
KUSTOMIZE_BUILD_ARGS=(--load-restrictor LoadRestrictionsNone)

kustomize build "${KUSTOMIZE_BUILD_ARGS[@]}" config/default/bootstrap > "${OUT_DIR}/bootstrap-components.yaml"
kustomize build "${KUSTOMIZE_BUILD_ARGS[@]}" config/default/control-plane > "${OUT_DIR}/control-plane-components.yaml"
kustomize build "${KUSTOMIZE_BUILD_ARGS[@]}" config/default/infrastructure > "${OUT_DIR}/infrastructure-components.yaml"

cp metadata.yaml "${OUT_DIR}/metadata.yaml"

# config/operator/*-provider.yamlはcluster-api-operatorのBootstrapProvider/ControlPlaneProvider/
# InfrastructureProvider CRのサンプルであり、versionのプレースホルダーを実際のrelease tagへ
# 置換してassetとして同梱する。
for provider in bootstrap control-plane infrastructure; do
  sed "s#REPLACE_WITH_RELEASE_VERSION#${RELEASE_TAG:-latest}#" \
    "${REPO_ROOT}/config/operator/${provider}-provider.yaml" > "${OUT_DIR}/${provider}-provider.yaml"
done
