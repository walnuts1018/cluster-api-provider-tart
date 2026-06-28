#!/usr/bin/env bash
set -euo pipefail

IMAGE_KEY="${IMAGE_KEY:?IMAGE_KEY is required}"
CLOUD_IMAGE_URL="${CLOUD_IMAGE_URL:?CLOUD_IMAGE_URL is required}"
IMAGE_ARCH="${IMAGE_ARCH:-amd64}"
OUTPUT_DIR="${OUTPUT_DIR:-dist/os-images/${IMAGE_KEY}}"
OUTPUT_IMAGE_NAME="${OUTPUT_IMAGE_NAME:-${IMAGE_KEY}.raw}"
K3S_VERSION="${K3S_VERSION:-}"
RAW_DISK_SIZE="${RAW_DISK_SIZE:-12G}"

work_dir="$(mktemp -d)"
cleanup() {
  rm -rf "${work_dir}"
}
trap cleanup EXIT

mkdir -p "${OUTPUT_DIR}"

source_image="${work_dir}/source.img"
custom_image="${work_dir}/custom.qcow2"
raw_image="${OUTPUT_DIR}/${OUTPUT_IMAGE_NAME}"

curl -fsSL "${CLOUD_IMAGE_URL}" -o "${source_image}"
qemu-img convert -O qcow2 "${source_image}" "${custom_image}"
qemu-img resize "${custom_image}" "${RAW_DISK_SIZE}"

k3s_install_env="INSTALL_K3S_SKIP_START=true INSTALL_K3S_SKIP_ENABLE=true"
if [ -n "${K3S_VERSION}" ]; then
  k3s_install_env="${k3s_install_env} INSTALL_K3S_VERSION=${K3S_VERSION}"
fi

virt-customize \
  -a "${custom_image}" \
  --update \
  --install curl,ca-certificates,open-iscsi,iptables,socat,conntrack,cloud-init \
  --run-command "curl -sfL https://get.k3s.io | ${k3s_install_env} sh -" \
  --truncate /etc/machine-id \
  --run-command "cloud-init clean --logs || true"

qemu-img convert -O raw "${custom_image}" "${raw_image}"
qemu-img info "${raw_image}"

cat > "${OUTPUT_DIR}/manifest.json" <<EOF
{
  "key": "${IMAGE_KEY}",
  "kind": "k3s",
  "arch": "${IMAGE_ARCH}",
  "sourceImage": "${CLOUD_IMAGE_URL}",
  "k3sVersion": "${K3S_VERSION}",
  "diskSize": "${RAW_DISK_SIZE}",
  "image": "${OUTPUT_IMAGE_NAME}"
}
EOF
