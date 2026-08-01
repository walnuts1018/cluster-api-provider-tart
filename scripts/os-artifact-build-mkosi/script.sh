#!/usr/bin/env bash
# ローカルのOS Artifactをmkosiで組み立てる。
set -euo pipefail
if [ "$(uname -s)" != Linux ]; then
  echo "artifact-build-mkosi requires Linux" >&2
  exit 1
fi
config="${ARTIFACT_CONFIG:-artifact/config/ubuntu-24.04-amd64-kubeadm.json}"
lock="${ARTIFACT_LOCK:-artifact/locks/amd64.json}"
os_family="$(jq -er '.os.family' "$config")"
os_version="$(jq -er '.os.version' "$config")"
architecture="$(jq -er '.architecture' "$config")"
distribution="$(jq -er '.kubernetes.distribution' "$config")"
source_date_epoch="$(jq -er '.sourceDateEpoch' "$lock")"
snapshot="$(jq -er '.snapshot' "$lock")"
case "$os_family:$os_version" in
  ubuntu:24.04) mkosi_release="noble" ;;
  ubuntu:26.04) mkosi_release="resolute" ;;
  debian:13) mkosi_release="trixie" ;;
  *)
    echo "unsupported artifact OS: $os_family $os_version" >&2
    exit 1
    ;;
esac
case "$architecture" in
  amd64) mkosi_architecture="x86-64"; go_architecture="amd64" ;;
  arm64) mkosi_architecture="arm64"; go_architecture="arm64" ;;
  *)
    echo "unsupported artifact architecture: $architecture" >&2
    exit 1
    ;;
esac
case "$distribution" in
  kubeadm|k0s) ;;
  *)
    echo "unsupported artifact Kubernetes distribution: $distribution" >&2
    exit 1
    ;;
esac
extra_dir="artifact/mkosi/mkosi.extra"
runtime_input_dir="${ARTIFACT_RUNTIME_INPUT_DIR:-dist/runtime-inputs}"
runtime_lock="${ETCDCTL_LOCK:-artifact/locks/etcdctl-linux-amd64.json}"
etcdctl_archive="$(jq -er '.files[0].name' "$runtime_lock")"
etcdctl_root="${etcdctl_archive%.tar.gz}"
workspace_dir="${ARTIFACT_MKOSI_WORKSPACE_DIR:-}"
mkdir -p "$extra_dir/usr/bin"
CGO_ENABLED=0 GOOS=linux GOARCH="$go_architecture" go build -o "$extra_dir/usr/bin/provisioning-agent" ./cmd/provisioning-agent
CGO_ENABLED=0 GOOS=linux GOARCH="$go_architecture" go build -o "$extra_dir/usr/bin/node-lifecycle-service" ./cmd/node-lifecycle-service
tar \
  --extract \
  --gzip \
  --file "$runtime_input_dir/$etcdctl_archive" \
  --strip-components=1 \
  --to-stdout \
  "$etcdctl_root/etcdctl" >"$extra_dir/usr/bin/etcdctl"
chmod 0755 "$extra_dir/usr/bin/etcdctl"
tar \
  --extract \
  --gzip \
  --file "$runtime_input_dir/$etcdctl_archive" \
  --strip-components=1 \
  --to-stdout \
  "$etcdctl_root/etcdutl" >"$extra_dir/usr/bin/etcdutl"
chmod 0755 "$extra_dir/usr/bin/etcdutl"
# build contextへ一時配置したbinaryをsource treeへ残さない。
trap 'rm -f "$extra_dir/usr/bin/provisioning-agent" "$extra_dir/usr/bin/node-lifecycle-service" "$extra_dir/usr/bin/etcdctl" "$extra_dir/usr/bin/etcdutl"' EXIT
set -- \
  --directory artifact/mkosi \
  --distribution "$os_family" \
  --release "$mkosi_release" \
  --architecture "$mkosi_architecture" \
  --output "tart-$os_family-$os_version-$architecture-$distribution" \
  --snapshot "$snapshot" \
  --source-date-epoch "$source_date_epoch"
if [ -n "$workspace_dir" ]; then
  set -- "$@" --workspace-directory "$workspace_dir"
fi
if [ "$distribution" = k0s ]; then
  set -- "$@" \
    --remove-package kubeadm \
    --remove-package kubectl \
    --remove-package kubelet \
    --remove-package kubernetes-cni \
    --remove-package cri-tools
fi
if [ "$os_family" = debian ]; then
	set -- "$@" \
		--package linux-image-amd64
fi
mkosi "$@" build
bash artifact/mkosi/normalize-output.sh \
  artifact/mkosi/mkosi.output \
  "${ARTIFACT_OUTPUT_DIR:-dist/os-artifact}"
go run ./cmd/mkosi-sbom \
  -input "${ARTIFACT_OUTPUT_DIR:-dist/os-artifact}/packages.json" \
  -output "${ARTIFACT_OUTPUT_DIR:-dist/os-artifact}/sbom.cdx.json"
