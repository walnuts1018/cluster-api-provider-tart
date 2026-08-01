#!/usr/bin/env bash
# ローカルのProvisioning E2E用k3sとネットワークを準備する。
set -euo pipefail
# k3sが未導入の場合だけlock fileで固定したinstallerから導入する。
if ! command -v k3s &> /dev/null; then
  echo "Installing k3s..."
  k3s_lock=artifact/locks/k3s-e2e.json
  k3s_version=$(jq -er '.version' "$k3s_lock")
  k3s_installer_url=$(jq -er '.installer.url' "$k3s_lock")
  k3s_installer_sha256=$(jq -er '.installer.sha256' "$k3s_lock")
  k3s_installer=$(mktemp)
  trap 'rm -f "$k3s_installer"' EXIT
  curl --fail --silent --show-error --location \
    "$k3s_installer_url" --output "$k3s_installer"
  echo "$k3s_installer_sha256  $k3s_installer" | sha256sum --check --strict
  sudo env INSTALL_K3S_VERSION="$k3s_version" sh "$k3s_installer"
  sleep 10
else
  echo "k3s is already installed."
fi
mkdir -p ~/.kube
sudo cp /etc/rancher/k3s/k3s.yaml ~/.kube/config
sudo chown "$(id -u):$(id -g)" ~/.kube/config
chmod 600 ~/.kube/config

# QEMUのProvisioning用Linux bridgeを作成する。
if ! ip link show br0 &> /dev/null; then
  echo "Creating Linux bridge br0..."
  sudo ip link add br0 type bridge
  sudo ip addr add 192.168.100.1/24 dev br0
  sudo ip link set br0 up
  sudo sysctl -w net.ipv4.ip_forward=1
  # default routeのinterfaceだけをNAT対象にし、runner固有のinterface名へ依存しない。
  MAIN_IF=$(ip route | grep default | sed -e "s/^.*dev.//" -e "s/.proto.*//")
  sudo iptables -t nat -A POSTROUTING -o "$MAIN_IF" -j MASQUERADE || true
  sudo iptables -A FORWARD -i br0 -j ACCEPT || true
  sudo iptables -A FORWARD -o br0 -m state --state RELATED,ESTABLISHED -j ACCEPT || true
  # QEMU bridge helperがbr0を利用できるようにする。
  sudo mkdir -p /etc/qemu
  if ! sudo grep -Fxq 'allow br0' /etc/qemu/bridge.conf 2>/dev/null; then
    printf '%s\n' 'allow br0' | sudo tee -a /etc/qemu/bridge.conf >/dev/null
  fi
else
  echo "Bridge br0 already exists."
fi

# lock fileで固定したiPXE binaryを準備する。
mkdir -p /tmp/tftpboot
if [ ! -f /tmp/tftpboot/ipxe-x86_64.efi ]; then
  echo "Downloading locked iPXE binaries..."
  go run ./cmd/locked-download -lock artifact/ipxe.lock.json -output-dir /tmp
  rm -rf /tmp/ipxeboot
  tar xzf /tmp/ipxeboot.tar.gz -C /tmp
  mv /tmp/ipxeboot/x86_64/ipxe.efi /tmp/tftpboot/ipxe-x86_64.efi
  rm -rf /tmp/ipxeboot /tmp/ipxeboot.tar.gz
fi
cat > /tmp/tftpboot/autoexec.ipxe <<'IPXE'
#!ipxe
dhcp || exit
chain --autofree http://${next-server}:8082/ipxe?mac=${net0/mac} || exit
IPXE

# iPXE chainload用のdnsmasqを起動する。
if ! pgrep -af "dnsmasq.*br0" &> /dev/null; then
  echo "Starting dnsmasq on br0..."
  sudo dnsmasq \
    --interface=br0 \
    --bind-interfaces \
    --port=0 \
    --dhcp-range=192.168.100.50,192.168.100.200,255.255.255.0,12h \
    --dhcp-option=option:router,192.168.100.1 \
    --dhcp-userclass=set:ipxe,iPXE \
    --dhcp-match=set:ipxe,175 \
    --dhcp-host=00:00:5e:00:53:00,set:e2ehost0,192.168.100.93 \
    --dhcp-host=00:00:5e:00:53:01,set:e2ehost1,192.168.100.94 \
    --dhcp-host=00:00:5e:00:53:02,set:e2ehost2,192.168.100.95 \
    --enable-tftp \
    --tftp-root=/tmp/tftpboot \
    --dhcp-boot=tag:!ipxe,ipxe-x86_64.efi \
    --dhcp-boot=tag:ipxe,tag:e2ehost0,http://192.168.100.1:8082/ipxe?mac=00:00:5e:00:53:00 \
    --dhcp-boot=tag:ipxe,tag:e2ehost1,http://192.168.100.1:8082/ipxe?mac=00:00:5e:00:53:01 \
    --dhcp-boot=tag:ipxe,tag:e2ehost2,http://192.168.100.1:8082/ipxe?mac=00:00:5e:00:53:02 \
    --dhcp-leasefile=/tmp/dnsmasq.leases \
    --log-dhcp \
    --keep-in-foreground &
  sleep 2
else
  echo "dnsmasq is already running on br0."
fi
