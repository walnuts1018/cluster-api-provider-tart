#!/usr/bin/env bash
set -euo pipefail

IMAGE_KEY="${IMAGE_KEY:?IMAGE_KEY is required}"
IMAGE_BUILDER_TARGET="${IMAGE_BUILDER_TARGET:?IMAGE_BUILDER_TARGET is required}"
KUBERNETES_VERSION="${KUBERNETES_VERSION:?KUBERNETES_VERSION is required}"
OUTPUT_DIR="${OUTPUT_DIR:-dist/os-images/${IMAGE_KEY}}"
OUTPUT_IMAGE_NAME="${OUTPUT_IMAGE_NAME:-${IMAGE_KEY}.raw}"
IMAGE_BUILDER_REF="${IMAGE_BUILDER_REF:-main}"
IMAGE_BUILDER_DIR="${IMAGE_BUILDER_DIR:-}"

work_dir="$(mktemp -d)"
cleanup() {
  rm -rf "${work_dir}"
}
trap cleanup EXIT

if [ -z "${IMAGE_BUILDER_DIR}" ]; then
  IMAGE_BUILDER_DIR="${work_dir}/image-builder"
  curl -fsSL "https://github.com/kubernetes-sigs/image-builder/archive/${IMAGE_BUILDER_REF}.tar.gz" -o "${work_dir}/image-builder.tar.gz"
  mkdir -p "${IMAGE_BUILDER_DIR}"
  tar xzf "${work_dir}/image-builder.tar.gz" --strip-components 1 -C "${IMAGE_BUILDER_DIR}"
fi

capi_dir="${IMAGE_BUILDER_DIR}/images/capi"
if [ ! -d "${capi_dir}" ]; then
  echo "image-builder images/capi directory not found: ${capi_dir}" >&2
  exit 1
fi

mkdir -p "${OUTPUT_DIR}"

pushd "${capi_dir}" >/dev/null
make deps-raw

kubernetes_no_v="${KUBERNETES_VERSION#v}"
kubernetes_series="v$(printf '%s' "${kubernetes_no_v}" | awk -F. '{print $1 "." $2}')"
export PACKER_FLAGS="${PACKER_FLAGS:-} --var kubernetes_semver=${KUBERNETES_VERSION} --var kubernetes_deb_version=${kubernetes_no_v}-1.1 --var kubernetes_series=${kubernetes_series}"

make "${IMAGE_BUILDER_TARGET}"

raw_image="$(find output -type f \( -name '*.raw' -o -name '*.img' \) -print | sort | tail -n 1)"
if [ -z "${raw_image}" ]; then
  echo "raw image was not created by image-builder target ${IMAGE_BUILDER_TARGET}" >&2
  exit 1
fi

qemu-img info "${raw_image}"
cp "${raw_image}" "${OUTPUT_DIR}/${OUTPUT_IMAGE_NAME}"
popd >/dev/null

cat > "${OUTPUT_DIR}/manifest.json" <<EOF
{
  "key": "${IMAGE_KEY}",
  "kind": "kubeadm",
  "builder": "kubernetes-sigs/image-builder",
  "imageBuilderRef": "${IMAGE_BUILDER_REF}",
  "imageBuilderTarget": "${IMAGE_BUILDER_TARGET}",
  "kubernetesVersion": "${KUBERNETES_VERSION}",
  "image": "${OUTPUT_IMAGE_NAME}"
}
EOF
