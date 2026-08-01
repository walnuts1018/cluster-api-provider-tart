#!/usr/bin/env bash
# Provisioning E2Eに必要なLinux依存パッケージを導入する。
set -euo pipefail
if ! command -v apt-get >/dev/null 2>&1; then
  echo "setup-provisioning-e2e-host supports Debian/Ubuntu hosts only" >&2
  exit 1
fi
sudo apt-get update
sudo apt-get install -y qemu-kvm libvirt-daemon-system libvirt-clients bridge-utils ovmf dnsmasq iproute2 tcpdump linux-image-generic initramfs-tools-core cpio ipxe busybox-static
# Provisioning simulatorは実際のWake-on-LAN UDP port 9をlistenする。
sudo sysctl -w net.ipv4.ip_unprivileged_port_start=0
