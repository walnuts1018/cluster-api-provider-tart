#!/usr/bin/env bash
# 実機向けProvisioning Agent initramfsを組み立てる。
set -euo pipefail
: "${AGENT_KERNEL:?AGENT_KERNEL is required}"
: "${AGENT_INITRD:?AGENT_INITRD is required}"
: "${SYSTEMD_BOOT_EFI:?SYSTEMD_BOOT_EFI is required}"
kernel_release="${AGENT_KERNEL_RELEASE:-${AGENT_KERNEL##*/vmlinuz-}}"
if [ "$kernel_release" = "$AGENT_KERNEL" ]; then
  echo "AGENT_KERNEL_RELEASE is required when AGENT_KERNEL is not named vmlinuz-<release>." >&2
  exit 1
fi
modules_root="${AGENT_MODULES_ROOT:-/}"
modules_dir="${modules_root%/}/lib/modules/$kernel_release"
firmware_dir="${AGENT_FIRMWARE_DIR:-${modules_root%/}/lib/firmware}"
if [ ! -f "$modules_dir/modules.dep" ]; then
  echo "Kernel modules for $kernel_release are required at $modules_dir." >&2
  exit 1
fi
for command in modprobe modinfo depmod unmkinitramfs; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "$command is required to build the Agent initramfs." >&2
    exit 1
  fi
done
busybox_path="${BUSYBOX_PATH:-$(command -v busybox || true)}"
: "${busybox_path:?BUSYBOX_PATH or busybox is required}"
output_dir="${AGENT_ARTIFACT_OUTPUT_DIR:-dist/agent-artifact}"
work_dir="$(mktemp -d)"
cleanup() { rm -rf "$work_dir"; }
trap cleanup EXIT

copy_module() {
  source_path="$1"
  compressed_target="$work_dir/root$source_path"

  case "$source_path" in
    *.ko.zst)
      command -v zstd >/dev/null 2>&1 || {
        echo "zstd is required to include $source_path as an uncompressed kernel module." >&2
        return 1
      }
      rm -f "$compressed_target"
      mkdir -p "$(dirname "${compressed_target%.zst}")"
      zstd -d -q -c "$source_path" > "${compressed_target%.zst}"
      chmod 0644 "${compressed_target%.zst}"
      ;;
    *.ko.xz)
      command -v xz >/dev/null 2>&1 || {
        echo "xz is required to include $source_path as an uncompressed kernel module." >&2
        return 1
      }
      rm -f "$compressed_target"
      mkdir -p "$(dirname "${compressed_target%.xz}")"
      xz -d -c "$source_path" > "${compressed_target%.xz}"
      chmod 0644 "${compressed_target%.xz}"
      ;;
    *.ko.gz)
      rm -f "$compressed_target"
      mkdir -p "$(dirname "${compressed_target%.gz}")"
      gzip -d -c "$source_path" > "${compressed_target%.gz}"
      chmod 0644 "${compressed_target%.gz}"
      ;;
    *) install -D -m 0644 "$source_path" "$compressed_target" ;;
  esac
}

mkdir -p "$output_dir" "$work_dir/root"/bin "$work_dir/root"/dev "$work_dir/root"/etc/tart "$work_dir/root"/proc "$work_dir/root"/sys "$work_dir/root"/run "$work_dir/root"/tmp "$work_dir/root"/usr/lib/systemd/boot/efi
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$work_dir/root/bin/provisioning-agent" ./cmd/provisioning-agent
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$work_dir/root/bin/efi-commit" ./cmd/efi-commit
cp "$busybox_path" "$work_dir/root/bin/busybox"
for applet in sh mount umount mkdir cat ln poweroff halt sleep udhcpc ifconfig route modprobe blockdev sfdisk udevadm mkfs.ext4 install sync reboot wget; do ln -sf busybox "$work_dir/root/bin/$applet"; done
cp "$SYSTEMD_BOOT_EFI" "$work_dir/root/usr/lib/systemd/boot/efi/systemd-bootx64.efi"
mkdir -p "$work_dir/root/lib/modules/$kernel_release"
cat > "$work_dir/agent-modules" <<'MODULES'
# Debian と Ubuntu の installer で一般的な PCI・仮想 NIC。
e1000
e1000e
igb
igc
ixgbe
ixgbevf
i40e
iavf
ice
bnxt_en
tg3
bnx2
bnx2x
qede
be2net
mlx4_en
mlx5_core
r8169
alx
atl1c
atl1e
atlantic
sky2
sis900
tulip
via-rhine
pcnet32
vmxnet3
virtio_net
hv_netvsc
ena
# Provisioning で利用する USB Ethernet adapter。
usbnet
cdc_ether
cdc_ncm
r8152
asix
ax88179_178a
smsc95xx
# SATA、NVMe、SCSI、RAID、USB、仮想 storage controller。
ahci
ata_piix
nvme
virtio_blk
virtio_scsi
megaraid_sas
mpt3sas
hpsa
smartpqi
aacraid
arcmsr
3w_9xxx
3w_sas
aic94xx
pm80xx
mvsas
hisi_sas
usb-storage
uas
sd_mod
sr_mod
scsi_mod
# 一般的な installer layout で必要な device mapper、software RAID、filesystem。
md_mod
raid0
raid1
raid10
linear
dm_mod
dm_crypt
ext4
xfs
btrfs
vfat
fat
exfat
ntfs3
MODULES
: > "$work_dir/root/etc/tart/agent-modules"
while IFS= read -r module; do
  case "$module" in
    ""|\#*) continue ;;
  esac
  if ! dependencies="$(modprobe --show-depends -d "$modules_root" -S "$kernel_release" "$module" 2>&1)"; then
    printf 'Skipping unavailable Agent module: %s\n' "$module" >&2
    continue
  fi
  printf '%s\n' "$module" >> "$work_dir/root/etc/tart/agent-modules"
  printf '%s\n' "$dependencies" | while IFS= read -r dependency; do
    case "$dependency" in
      "insmod "*) module_path="${dependency#insmod }" ;;
      *) continue ;;
    esac
    module_path="${module_path%% *}"
    copy_module "$module_path"
    modinfo -F firmware "$module_path" 2>/dev/null | while IFS= read -r firmware; do
      [ -n "$firmware" ] || continue
      if [ -f "$firmware_dir/$firmware" ]; then
        install -D -m 0644 "$firmware_dir/$firmware" "$work_dir/root/lib/firmware/$firmware"
      fi
    done
  done
done < "$work_dir/agent-modules"
depmod -b "$work_dir/root" "$kernel_release"
if [ ! -s "$work_dir/root/etc/tart/agent-modules" ]; then
  echo "No supported Agent hardware modules were found for $kernel_release." >&2
  exit 1
fi
if [ -n "${AGENT_ARTIFACT_MODULES_FILE:-}" ]; then
  cp "$work_dir/root/etc/tart/agent-modules" "$AGENT_ARTIFACT_MODULES_FILE"
fi
cat > "$work_dir/root/bin/udhcpc-script" <<'UDHCPC'
#!/bin/sh
set -eu

case "${1:-}" in
  bound|renew)
    ifconfig "$interface" "$ip" netmask "$subnet" up
    if [ -n "${router:-}" ]; then
      for gateway in $router; do
        route add default gw "$gateway" dev "$interface" || true
      done
    fi
    ;;
esac
UDHCPC
cat > "$work_dir/root/init" <<'INIT'
#!/bin/sh
set -eux
PATH=/bin:/sbin:/usr/bin:/usr/sbin
export PATH
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev
exec </dev/console >/dev/console 2>&1

failure_shell() {
  exit_code=$?
  trap - EXIT
  printf '\nTart Provisioning Agent init failed with exit code %s.\n' "$exit_code"
  printf '%s\n' 'An interactive shell is available for diagnosis. Run poweroff to shut down.'
  while :; do
    /bin/sh
  done
}
trap failure_shell EXIT

mkdir -p /dev/disk/by-id /etc /run /tmp
while IFS= read -r module; do
  [ -n "$module" ] || continue
  if ! modprobe "$module"; then
    printf 'Unable to load Agent hardware module: %s\n' "$module" >&2
  fi
done < /etc/tart/agent-modules
sleep 2
for iface in /sys/class/net/*; do
  iface="${iface##*/}"
  [ "$iface" != lo ] || continue
  ifconfig "$iface" up || true
  udhcpc -i "$iface" -q -n -t 5 -T 3 -s /bin/udhcpc-script || true
done
system_uuid=""
trust_url=""
for arg in $(cat /proc/cmdline); do case "$arg" in tart.agent.host-uid=*) system_uuid="${arg#tart.agent.host-uid=}";; tart.agent.trust-url=*) trust_url="${arg#tart.agent.trust-url=}";; esac; done
[ -n "$trust_url" ]
# 初回bootは隔離Provisioning L2だけを前提にし、ここで取得した公開鍵で以後のHTTPS通信とPlanを検証する。
wget -O /etc/tart/agent-tls.crt "$trust_url/v1/agent-trust/agent-api-ca.pem"
wget -O /etc/tart/agent-plan-public.pem "$trust_url/v1/agent-trust/agent-plan-public.pem"
wget -O /etc/tart/os-artifact-public.pem "$trust_url/v1/agent-trust/os-artifact-public.pem"
/bin/provisioning-agent --provision --system-uuid="$system_uuid" --tls-ca-file=/etc/tart/agent-tls.crt --plan-key-id=install-plan-v1 --plan-key-file=/etc/tart/agent-plan-public.pem --artifact-key-id=release-v1 --artifact-key-file=/etc/tart/os-artifact-public.pem --efi-commit-driver=/bin/efi-commit
poweroff -f || halt -f
INIT
chmod 0755 "$work_dir/root/init"
chmod 0755 "$work_dir/root/bin/udhcpc-script"
(cd "$work_dir/root" && find . -print0 | cpio --null -ov --format=newc 2>/dev/null | gzip -9) > "$work_dir/overlay.initrd"
if [ -n "${AGENT_ARTIFACT_OVERLAY_INITRD:-}" ]; then
  cp "$work_dir/overlay.initrd" "$AGENT_ARTIFACT_OVERLAY_INITRD"
fi
cp "$AGENT_KERNEL" "$output_dir/vmlinuz"
unmkinitramfs "$AGENT_INITRD" "$work_dir/base-initramfs"
if [ ! -d "$work_dir/base-initramfs/main" ]; then
  echo "Agent base initramfs does not contain a main filesystem." >&2
  exit 1
fi
# BusyBoxのmodprobeはmoduleのbytesを展開せずkernelへ渡すため、base側の圧縮重複を除去し、
# depmodが選択済みのすべてのmoduleをELF copyへ解決するようにする。
while IFS= read -r module_path; do
  relative_module_path="${module_path#"$work_dir/root/"}"
  rm -f "$work_dir/base-initramfs/main/${relative_module_path}.zst" \
    "$work_dir/base-initramfs/main/${relative_module_path}.xz" \
    "$work_dir/base-initramfs/main/${relative_module_path}.gz"
done < <(find "$work_dir/root/lib/modules" -type f -name '*.ko')
for source in "$work_dir/root"/* "$work_dir/root"/.[!.]* "$work_dir/root"/..?*; do
  [ -e "$source" ] || [ -L "$source" ] || continue
  target="$work_dir/base-initramfs/main/${source##*/}"
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
(cd "$work_dir/base-initramfs/main" && find . -print0 | cpio --null -ov --format=newc 2>/dev/null | gzip -9) > "$output_dir/initrd"
if ! gzip -dc "$output_dir/initrd" | cpio -i --to-stdout init 2>/dev/null | grep -Fq '/bin/provisioning-agent'; then
  echo "Agent initramfs does not contain the Provisioning Agent init entrypoint." >&2
  exit 1
fi
