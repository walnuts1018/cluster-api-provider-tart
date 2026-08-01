#!/usr/bin/env bash
# Provisioning E2E失敗後の状態を収集し、診断コマンドの失敗で元の失敗を隠さない。
set -o nounset
set -o pipefail

artifacts_dir="${ARTIFACTS:-${PWD}/_artifacts}"
cluster_log_dir="${artifacts_dir}/clusters/tart-e2e"
mkdir -p "${cluster_log_dir}/cluster-api"

capture() {
  local output_path="$1"
  shift
  "$@" >"${output_path}" 2>&1 || true
}

if ! command -v kubectl >/dev/null 2>&1; then
  printf '%s\n' 'kubectl is unavailable; Kubernetes diagnostics were skipped.' >"${cluster_log_dir}/kubectl-unavailable.txt"
else
  capture "${cluster_log_dir}/all-resources.txt" kubectl get all --all-namespaces -o wide
  capture "${cluster_log_dir}/crds.txt" kubectl get crds
  capture "${cluster_log_dir}/controller-deployments.yaml" kubectl get deployments -n cluster-api-provider-tart-system -o yaml
  capture "${cluster_log_dir}/pods.txt" kubectl get pods --all-namespaces -o wide
  capture "${cluster_log_dir}/events.yaml" kubectl get events --all-namespaces --sort-by=.lastTimestamp -o yaml
  capture "${cluster_log_dir}/services.txt" kubectl get services --all-namespaces -o wide
  capture "${cluster_log_dir}/networkpolicies.txt" kubectl get networkpolicies --all-namespaces -o wide
  capture "${cluster_log_dir}/ipaddressclaims.txt" kubectl get ipaddressclaims --all-namespaces -o wide
  capture "${cluster_log_dir}/ipaddresspools.txt" kubectl get ipaddresspools --all-namespaces -o wide

  resources=(
    clusters.cluster.x-k8s.io
    clusterclasses.cluster.x-k8s.io
    machines.cluster.x-k8s.io
    machinesets.cluster.x-k8s.io
    machinedeployments.cluster.x-k8s.io
    machinepools.cluster.x-k8s.io
    machinehealthchecks.cluster.x-k8s.io
    kubeadmcontrolplanes.controlplane.cluster.x-k8s.io
    kubeadmconfigs.bootstrap.cluster.x-k8s.io
    kubeadmconfigtemplates.bootstrap.cluster.x-k8s.io
    tartclusters.infrastructure.cluster.x-k8s.io
    tarthosts.infrastructure.cluster.x-k8s.io
    tarthostoperations.infrastructure.cluster.x-k8s.io
    tartmachines.infrastructure.cluster.x-k8s.io
    tartmachinetemplates.infrastructure.cluster.x-k8s.io
  )
  for resource in "${resources[@]}"; do
    capture "${cluster_log_dir}/cluster-api/${resource}.yaml" kubectl get "${resource}" --all-namespaces -o yaml
  done

  while IFS= read -r pod; do
    [ -n "${pod}" ] || continue
    capture "${cluster_log_dir}/controller-logs-${pod}.log" kubectl logs "${pod}" -n cluster-api-provider-tart-system --tail=1000
    capture "${cluster_log_dir}/controller-logs-${pod}-previous.log" kubectl logs "${pod}" -n cluster-api-provider-tart-system --previous --tail=1000
    capture "${cluster_log_dir}/pod-description-${pod}.txt" kubectl describe pod "${pod}" -n cluster-api-provider-tart-system
    capture "${cluster_log_dir}/pod-events-${pod}.txt" kubectl get events --field-selector "involvedObject.name=${pod}" -n cluster-api-provider-tart-system --sort-by=.lastTimestamp
  done < <(kubectl get pods -l control-plane=controller-manager -n cluster-api-provider-tart-system -o jsonpath='{.items[*].metadata.name}' 2>/dev/null | tr ' ' '\n')
fi

capture "${cluster_log_dir}/dnsmasq-process.txt" pgrep -af dnsmasq
capture "${cluster_log_dir}/dnsmasq-leases.txt" cat /tmp/dnsmasq.leases
capture "${cluster_log_dir}/bridge-info.txt" ip link show br0
capture "${cluster_log_dir}/bridge-addresses.txt" ip addr show br0
capture "${cluster_log_dir}/route-table.txt" ip route
capture "${cluster_log_dir}/iptables-nat.txt" sudo iptables -t nat -L -n -v
capture "${cluster_log_dir}/iptables-filter.txt" sudo iptables -L -n -v
capture "${cluster_log_dir}/qemu-status.txt" pgrep -af qemu
capture "${cluster_log_dir}/network-interfaces.txt" ip addr

find . -maxdepth 1 -type f \( -name 'qemu-output-*.log' -o -name '*.pcap' \) -exec cp {} "${cluster_log_dir}/" \; 2>/dev/null || true
