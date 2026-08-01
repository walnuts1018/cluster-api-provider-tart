#!/usr/bin/env bash
# Release用のinitramfsは実行中runnerのカーネルと同じmodule treeから組み立てる。
set -euo pipefail

: "${AGENT_ARTIFACT_SIGNING_KEY:?AGENT_ARTIFACT_SIGNING_KEY is required}"
: "${AGENT_ARTIFACT_SIGNING_KEY_ID:?AGENT_ARTIFACT_SIGNING_KEY_ID is required}"
: "${AGENT_REPOSITORY:?AGENT_REPOSITORY is required}"
: "${AGENT_TAG:?AGENT_TAG is required}"
: "${RUNNER_TEMP:?RUNNER_TEMP is required}"

kernel_path="$(find /boot -maxdepth 1 -type f -name 'vmlinuz-*' -print | sort | tail -n 1)"
initrd_path="$(find /boot -maxdepth 1 -type f -name 'initrd.img-*' -print | sort | tail -n 1)"
test -n "${kernel_path}"
test -n "${initrd_path}"

sudo install -m 0644 "${kernel_path}" /tmp/tart-agent-vmlinuz
sudo install -m 0644 "${initrd_path}" /tmp/tart-agent-initrd
export AGENT_KERNEL_RELEASE="${kernel_path##*/vmlinuz-}"
export AGENT_KERNEL=/tmp/tart-agent-vmlinuz
export AGENT_INITRD=/tmp/tart-agent-initrd
export AGENT_ARTIFACT_MODULES_FILE=/tmp/tart-agent-modules
export AGENT_ARTIFACT_OVERLAY_INITRD=/tmp/tart-agent-overlay.initrd
export SYSTEMD_BOOT_EFI=/usr/lib/systemd/boot/efi/systemd-bootx64.efi
export AGENT_ARTIFACT_OUTPUT_DIR=dist/agent-artifact
mise run agent-artifact-build-real

gzip -dc /tmp/tart-agent-overlay.initrd > /tmp/tart-agent-overlay.cpio
cpio -it < /tmp/tart-agent-overlay.cpio > /tmp/tart-agent-initramfs-files
grep -Fx 'etc/tart/agent-modules' /tmp/tart-agent-initramfs-files
grep -Fx e1000e /tmp/tart-agent-modules
grep -Fx nvme /tmp/tart-agent-modules
rm -rf /tmp/tart-agent-final-initramfs
mkdir /tmp/tart-agent-final-initramfs
(
  cd /tmp/tart-agent-final-initramfs
  gzip -dc "${GITHUB_WORKSPACE}/dist/agent-artifact/initrd" | cpio -idmu
)
grep -Fx 'exec </dev/console >/dev/console 2>&1' /tmp/tart-agent-final-initramfs/init
test -x /tmp/tart-agent-final-initramfs/usr/bin/provisioning-agent
grep -Fq '/bin/provisioning-agent' /tmp/tart-agent-final-initramfs/init
while IFS= read -r module; do
  module_path="$(modinfo -b /tmp/tart-agent-final-initramfs -k "$AGENT_KERNEL_RELEASE" -F filename "$module")"
  case "$module_path" in
    "(builtin)"|*.ko) ;;
    *)
      echo "Agent module is neither built in nor stored as an uncompressed ELF module: $module ($module_path)" >&2
      exit 1
      ;;
  esac
done < /tmp/tart-agent-modules

payload_digest="$(go run ./cmd/artifacter -dir dist/agent-artifact -repo "${AGENT_REPOSITORY}" -tag "${AGENT_TAG}-payload")"
reference="oci://${AGENT_REPOSITORY}@${payload_digest}"
AGENT_ARTIFACT_REFERENCE="${reference}" \
  AGENT_ARTIFACT_KERNEL=dist/agent-artifact/vmlinuz \
  AGENT_ARTIFACT_INITRD=dist/agent-artifact/initrd \
  AGENT_ARTIFACT_OUTPUT_DIR=dist/agent-artifact \
  mise run agent-artifact-manifest
go run ./cmd/artifacter -dir dist/agent-artifact -repo "${AGENT_REPOSITORY}" -tag "${AGENT_TAG}"
printf 'AGENT_ARTIFACT_REFERENCE=%s\n' "${reference}" > dist/agent-artifact/references.env
