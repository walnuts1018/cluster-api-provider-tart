#!/usr/bin/env bash
# ローカルのProvisioning E2E用入力を安全に準備する。
set -euo pipefail

CONTAINER_TOOL="${CONTAINER_TOOL:-docker}"
registry_name="${PROVISIONING_E2E_REGISTRY_NAME:-capt-e2e-registry-55000}"
registry_host="${PROVISIONING_E2E_REGISTRY_HOST:-127.0.0.1:55000}"
output_dir="${PROVISIONING_E2E_OUTPUT_DIR:-dist/provisioning-e2e}"
generated_dir="config/provisioning-e2e/generated"
artifact_dir="$output_dir/artifact"
agent_artifact_dir="$generated_dir/agent-artifact"
agent_initramfs_dir="$output_dir/agent-initramfs"

if ! "$CONTAINER_TOOL" container inspect "$registry_name" >/dev/null 2>&1; then
  "$CONTAINER_TOOL" run -d -p "${registry_host#*:}:5000" --restart=always --name "$registry_name" registry:2 >/dev/null
elif ! "$CONTAINER_TOOL" ps --filter "name=$registry_name" --filter "status=running" --quiet | grep -q .; then
  "$CONTAINER_TOOL" start "$registry_name" >/dev/null
fi

mkdir -p "$output_dir" "$generated_dir" "$artifact_dir" "$agent_artifact_dir"
if [ ! -f "$output_dir/os-artifact-private.pem" ]; then
  openssl genpkey -algorithm Ed25519 -out "$output_dir/os-artifact-private.pem"
fi
if [ ! -f "$generated_dir/os-artifact-public.pem" ]; then
  openssl pkey -in "$output_dir/os-artifact-private.pem" -pubout -out "$generated_dir/os-artifact-public.pem"
fi
if [ ! -f "$generated_dir/agent-plan-private.pem" ]; then
  openssl genpkey -algorithm Ed25519 -out "$generated_dir/agent-plan-private.pem"
fi
openssl pkey \
  -in "$generated_dir/agent-plan-private.pem" \
  -pubout \
  -out "$output_dir/agent-plan-public.pem"
if [ ! -f "$output_dir/agent-artifact-private.pem" ]; then
  openssl genpkey -algorithm Ed25519 -out "$output_dir/agent-artifact-private.pem"
fi
openssl pkey \
  -in "$output_dir/agent-artifact-private.pem" \
  -pubout \
  -out "$generated_dir/agent-artifact-public.pem"
openssl req \
  -x509 \
  -newkey rsa:2048 \
  -sha256 \
  -nodes \
  -days 1 \
  -subj "/CN=192.168.100.1" \
  -addext "subjectAltName=IP:192.168.100.1,DNS:hoge.test.walnuts.dev" \
  -keyout "$generated_dir/agent-tls.key" \
  -out "$generated_dir/agent-tls.crt"

printf 'provisioning e2e os image\n' > "$output_dir/os.img"
printf 'provisioning e2e verity\n' > "$output_dir/os.verity"
printf 'provisioning e2e kernel\n' > "$output_dir/vmlinuz"
printf 'provisioning e2e initrd\n' > "$output_dir/initrd"
printf '{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"components":[]}\n' > "$output_dir/sbom.cdx.json"
printf '{"_type":"https://in-toto.io/Statement/v1","subject":[],"predicateType":"https://slsa.dev/provenance/v1","predicate":{}}\n' > "$output_dir/provenance.intoto.json"

agent_kernel="${PROVISIONING_E2E_AGENT_KERNEL:-}"
agent_base_initrd="${PROVISIONING_E2E_AGENT_INITRD:-}"
if [ -z "$agent_kernel" ]; then
  agent_kernel="$(find /boot -maxdepth 1 -type f -name 'vmlinuz-*' -print | sort -V | tail -n 1 || true)"
fi
if [ -z "$agent_base_initrd" ] && [ -n "$agent_kernel" ]; then
  kernel_version="${agent_kernel##*/vmlinuz-}"
  if [ -f "/boot/initrd.img-$kernel_version" ]; then
    agent_base_initrd="/boot/initrd.img-$kernel_version"
  fi
fi
if [ ! -f "$agent_kernel" ] || [ ! -f "$agent_base_initrd" ]; then
  echo "Linux kernel and initrd are required for provisioning E2E Agent Artifact." >&2
  echo "Set PROVISIONING_E2E_AGENT_KERNEL and PROVISIONING_E2E_AGENT_INITRD, or install linux-image-generic." >&2
  exit 1
fi
busybox_path="${PROVISIONING_E2E_BUSYBOX:-$(command -v busybox || true)}"
if [ ! -x "$busybox_path" ]; then
  echo "busybox is required for provisioning E2E Agent Artifact initramfs." >&2
  echo "Set PROVISIONING_E2E_BUSYBOX, or install busybox-static." >&2
  exit 1
fi
if ! command -v unmkinitramfs >/dev/null 2>&1; then
  echo "unmkinitramfs is required to merge the Provisioning E2E Agent initramfs." >&2
  exit 1
fi

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$output_dir/provisioning-agent" ./cmd/provisioning-agent
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$output_dir/efi-commit" ./cmd/efi-commit
rm -rf "$agent_initramfs_dir"
mkdir -p \
  "$agent_initramfs_dir/bin" \
  "$agent_initramfs_dir/dev" \
  "$agent_initramfs_dir/etc/tart" \
  "$agent_initramfs_dir/proc" \
  "$agent_initramfs_dir/sys" \
  "$agent_initramfs_dir/tmp"
cp "$output_dir/provisioning-agent" "$agent_initramfs_dir/bin/provisioning-agent"
cp "$output_dir/efi-commit" "$agent_initramfs_dir/bin/efi-commit"
cp "$busybox_path" "$agent_initramfs_dir/bin/busybox"
for applet in sh mount mkdir cat ln poweroff halt sleep udhcpc ifconfig route; do
  ln -sf busybox "$agent_initramfs_dir/bin/$applet"
done
cp "$output_dir/agent-plan-public.pem" "$agent_initramfs_dir/etc/tart/agent-plan-public.pem"
cp "$generated_dir/agent-tls.crt" "$agent_initramfs_dir/etc/tart/agent-tls.crt"
cat > "$agent_initramfs_dir/bin/udhcpc-script" <<'UDHCPC'
#!/bin/sh
set -eu

case "${1:-}" in
  bound|renew)
    ifconfig "$interface" "$ip" netmask "$subnet" up
    if [ -n "${router:-}" ]; then
      for gateway in $router; do
        route add default gw "$gateway" dev "$interface" || true
        break
      done
    fi
    if [ -n "${dns:-}" ]; then
      : > /etc/resolv.conf
      for nameserver in $dns; do
        echo "nameserver $nameserver" >> /etc/resolv.conf
      done
    fi
    ;;
esac
UDHCPC
cat > "$agent_initramfs_dir/init" <<'INIT'
#!/bin/sh
set -eu

PATH=/bin:/sbin:/usr/bin:/usr/sbin
export PATH

mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev
mkdir -p /dev/disk/by-id /etc /run /tmp

for iface in /sys/class/net/*; do
  iface="${iface##*/}"
  [ "$iface" != "lo" ] || continue
  ifconfig "$iface" up || true
  udhcpc -i "$iface" -q -n -t 5 -T 3 -s /bin/udhcpc-script || true
done

for device in /sys/class/block/*; do
  name="${device##*/}"
  serial="$(cat "$device/device/serial" 2>/dev/null || cat "$device/serial" 2>/dev/null || true)"
  [ -n "$serial" ] || continue
  ln -sf "../../$name" "/dev/disk/by-id/virtio-$serial"
done

system_uuid=""
for arg in $(cat /proc/cmdline); do
  case "$arg" in
    tart.agent.host-uid=*)
      system_uuid="${arg#tart.agent.host-uid=}"
      ;;
  esac
done

echo "tart e2e provisioning-agent preflight starting"
/bin/provisioning-agent \
  --preflight-only \
  --system-uuid="$system_uuid" \
  --tls-ca-file=/etc/tart/agent-tls.crt \
  --plan-key-id=e2e-agent-plan \
  --plan-key-file=/etc/tart/agent-plan-public.pem
echo "tart e2e provisioning-agent preflight completed"

poweroff -f || halt -f || sh
INIT
chmod 0755 "$agent_initramfs_dir/init"
chmod 0755 "$agent_initramfs_dir/bin/udhcpc-script"
sudo install -m 0644 "$agent_kernel" "$agent_artifact_dir/vmlinuz"
agent_base_initramfs_dir="$output_dir/agent-base-initramfs"
rm -rf "$agent_base_initramfs_dir"
unmkinitramfs "$agent_base_initrd" "$agent_base_initramfs_dir"
if [ ! -d "$agent_base_initramfs_dir/main" ]; then
  echo "Agent base initramfs does not contain a main filesystem." >&2
  exit 1
fi
for source in "$agent_initramfs_dir"/* "$agent_initramfs_dir"/.[!.]* "$agent_initramfs_dir"/..?*; do
  [ -e "$source" ] || [ -L "$source" ] || continue
  target="$agent_base_initramfs_dir/main/${source##*/}"
  if [ -d "$source" ]; then
    if [ -L "$target" ]; then
      cp -a "$source/." "$(readlink -f "$target")/"
    else
      mkdir -p "$target"
      cp -a "$source/." "$target/"
    fi
  else
    if [ -d "$target" ] && [ ! -L "$target" ]; then
      rm -rf -- "$target"
    else
      rm -f -- "$target"
    fi
    cp -a "$source" "$target"
  fi
done
(cd "$agent_base_initramfs_dir/main" && find . -print0 | cpio --null -ov --format=newc 2>/dev/null | gzip -9) > "$agent_artifact_dir/initrd"
if ! gzip -dc "$agent_artifact_dir/initrd" | cpio -i --to-stdout init 2>/dev/null | grep -Fq '/bin/provisioning-agent'; then
  echo "Provisioning E2E Agent initramfs does not contain the Agent init entrypoint." >&2
  exit 1
fi

ARTIFACT_IMAGE="$output_dir/os.img" \
ARTIFACT_VERITY="$output_dir/os.verity" \
ARTIFACT_KERNEL="$output_dir/vmlinuz" \
ARTIFACT_INITRD="$output_dir/initrd" \
ARTIFACT_VERITY_ROOT_HASH="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
ARTIFACT_SIGNING_KEY="$output_dir/os-artifact-private.pem" \
ARTIFACT_SIGNING_KEY_ID="e2e-os-artifact" \
ARTIFACT_OUTPUT_DIR="$artifact_dir" \
  mise run artifact-manifest

artifact_ref="$(
  ARTIFACT_REPOSITORY="$registry_host/tart/ubuntu" \
  ARTIFACT_TAG="e2e" \
  ARTIFACT_IMAGE="$output_dir/os.img" \
  ARTIFACT_VERITY="$output_dir/os.verity" \
  ARTIFACT_KERNEL="$output_dir/vmlinuz" \
  ARTIFACT_INITRD="$output_dir/initrd" \
  ARTIFACT_SBOM="$output_dir/sbom.cdx.json" \
  ARTIFACT_PROVENANCE="$output_dir/provenance.intoto.json" \
  ARTIFACT_OUTPUT_DIR="$artifact_dir" \
    mise run artifact-push | tail -n 1
)"

AGENT_ARTIFACT_REFERENCE="oci://$registry_host/tart/provisioning-agent@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" \
AGENT_ARTIFACT_KERNEL="$agent_artifact_dir/vmlinuz" \
AGENT_ARTIFACT_INITRD="$agent_artifact_dir/initrd" \
AGENT_ARTIFACT_SIGNING_KEY="$output_dir/agent-artifact-private.pem" \
AGENT_ARTIFACT_SIGNING_KEY_ID="e2e-agent-artifact" \
AGENT_ARTIFACT_OUTPUT_DIR="$agent_artifact_dir" \
  mise run agent-artifact-manifest

{
  printf 'OS_ARTIFACT_REF=%s\n' "$artifact_ref"
  printf 'OS_ARTIFACT_REGISTRY=%s\n' "$registry_host"
} > "$output_dir/env"
