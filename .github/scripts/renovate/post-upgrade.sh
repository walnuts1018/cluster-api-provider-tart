#!/usr/bin/env bash

set -euo pipefail

# Renovateが更新するURLやバージョンから、検証対象の派生メタデータを再生成する。
root_dir="$(git rev-parse --show-toplevel)"
readonly root_dir
cd "$root_dir"

tmp_dir="$(mktemp -d)"
readonly tmp_dir
trap 'rm -rf "$tmp_dir"' EXIT

for command in curl gzip jq sha256sum stat; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "$command is required" >&2
    exit 1
  }
done

base_ref="origin/${GITHUB_BASE_REF:-main}"
if ! git rev-parse --verify --quiet "$base_ref" >/dev/null; then
  echo "base branch is unavailable: $base_ref" >&2
  exit 1
fi

changed_from_base() {
  ! git diff --quiet "$base_ref"...HEAD -- "$@"
}

file_size() {
  if stat --version 2>/dev/null | grep -q "GNU"; then
    stat -c '%s' "$1"
  else
    stat -f '%z' "$1"
  fi
}

refresh_url_lock() {
  local lock_file="$1"
  local url file_name downloaded size digest

  url="$(jq -er '.files[0].url' "$lock_file")"
  file_name="${url##*/}"
  downloaded="$tmp_dir/$file_name"
  curl --fail --silent --show-error --location "$url" --output "$downloaded"
  size="$(file_size "$downloaded")"
  digest="$(sha256sum "$downloaded" | awk '{print $1}')"
  jq --arg name "$file_name" --argjson size "$size" --arg sha256 "$digest" \
    '.files[0].name = $name | .files[0].sizeBytes = $size | .files[0].sha256 = $sha256' \
    "$lock_file" >"$tmp_dir/$(basename "$lock_file")"
  mv "$tmp_dir/$(basename "$lock_file")" "$lock_file"
}

if changed_from_base artifact/mkosi/mkosi.conf artifact/locks/k3s-e2e.json artifact/locks/etcdctl-linux-amd64.json artifact/ipxe.lock.json; then
k3s_lock='artifact/locks/k3s-e2e.json'
k3s_url="$(jq -er '.installer.url' "$k3s_lock")"
raw_host='raw.githubusercontent.com'
k3s_repo='k3s-io/k3s'
k3s_url_prefix="https://${raw_host}/${k3s_repo}/"
k3s_url_version="${k3s_url#"$k3s_url_prefix"}"
k3s_url_version="${k3s_url_version%%/install.sh}"
if [[ "$k3s_url_version" != v[0-9]* ]]; then
  echo "unsupported k3s installer URL: $k3s_url" >&2
  exit 1
fi
k3s_installer="$tmp_dir/k3s-install.sh"
curl --fail --silent --show-error --location "$k3s_url" --output "$k3s_installer"
k3s_sha256="$(sha256sum "$k3s_installer" | awk '{print $1}')"
jq --arg version "$k3s_url_version" --arg sha256 "$k3s_sha256" \
  '.version = $version | .installer.sha256 = $sha256' "$k3s_lock" >"$tmp_dir/k3s-e2e.json"
mv "$tmp_dir/k3s-e2e.json" "$k3s_lock"

# mkosi.confを一次情報にして、Kubernetes debの名前・URL・サイズ・digestを同期する。
kubernetes_series="$(sed -nE 's/.*kubeadm=([0-9]+\.[0-9]+)\..*/\1/p' artifact/mkosi/mkosi.conf | head -n 1)"
if [ -z "$kubernetes_series" ]; then
  echo 'kubeadm version is missing from artifact/mkosi/mkosi.conf' >&2
  exit 1
fi
files_json="$tmp_dir/kubernetes-files.jsonl"
: >"$files_json"
package_index="$tmp_dir/Packages"
for package in cri-tools kubeadm kubectl kubelet kubernetes-cni; do
  version="$(sed -nE "s/.*${package}=([^[:space:]]+).*/\1/p" artifact/mkosi/mkosi.conf | head -n 1)"
  if [ -z "$version" ]; then
    echo "${package} version is missing from artifact/mkosi/mkosi.conf" >&2
    exit 1
  fi
  name="${package}_${version}_amd64.deb"
  url="https://pkgs.k8s.io/core:/stable:/v${kubernetes_series}/deb/amd64/${name}"
  downloaded="$tmp_dir/$name"
  if ! curl --fail --silent --location "$url" --output "$downloaded"; then
    # DebianのリビジョンはKubernetesのタグと独立して更新されるため、旧suffixを再利用しない。
    if [ ! -s "$package_index" ]; then
      curl --fail --silent --show-error --location \
        "https://pkgs.k8s.io/core:/stable:/v${kubernetes_series}/deb/Packages.gz" |
        gzip -dc >"$package_index"
    fi
    base_version="${version%%-*}"
    version="$(awk -v package="$package" -v base_version="$base_version" '
      BEGIN { RS="" }
      $0 ~ "(^|\\n)Package: " package "(\\n|$)" {
        match($0, /(^|\n)Version: [^\n]+/)
        value=substr($0, RSTART, RLENGTH)
        sub(/^.*Version: /, "", value)
        if (value ~ ("^" base_version "(-|$)")) print value
      }
    ' "$package_index" | sort -V | tail -n 1)"
    if [ -z "$version" ]; then
      echo "unable to resolve ${package} package version for ${kubernetes_series}" >&2
      exit 1
    fi
    name="${package}_${version}_amd64.deb"
    url="https://pkgs.k8s.io/core:/stable:/v${kubernetes_series}/deb/amd64/${name}"
    downloaded="$tmp_dir/$name"
    curl --fail --silent --show-error --location "$url" --output "$downloaded"
    sed -i.bak -E "s|^([[:space:]]+${package}=)[^[:space:]]+|\\1${version}|" artifact/mkosi/mkosi.conf
    rm -f artifact/mkosi/mkosi.conf.bak
  fi
  size="$(file_size "$downloaded")"
  digest="$(sha256sum "$downloaded" | awk '{print $1}')"
  jq -n --arg name "$name" --arg url "$url" --argjson size "$size" --arg sha256 "$digest" \
    '{name: $name, url: $url, sizeBytes: $size, sha256: $sha256}' >>"$files_json"
done
jq --slurpfile files <(jq -s . "$files_json") '.files = $files[0]' \
  artifact/locks/amd64.json >"$tmp_dir/amd64.json"
mv "$tmp_dir/amd64.json" artifact/locks/amd64.json

refresh_url_lock artifact/locks/etcdctl-linux-amd64.json
refresh_url_lock artifact/ipxe.lock.json

# 取得済みdebもlockから再取得し、古いバージョンのファイルを残さない。
mkdir -p artifact/mkosi/mkosi.packages
find artifact/mkosi/mkosi.packages -type f -name '*.deb' -delete
go run ./cmd/locked-download \
  -lock artifact/locks/amd64.json \
  -output-dir artifact/mkosi/mkosi.packages
fi

if changed_from_base .github/workflows/release-artifacts.yaml; then
  mkosi_ref="$(sed -nE 's|.*uses: systemd/mkosi@([[:xdigit:]]+).*# v([0-9]+).*|\1 \2|p' .github/workflows/release-artifacts.yaml)"
  if [ -z "$mkosi_ref" ]; then
    echo 'systemd/mkosi action pin is missing from release-artifacts workflow' >&2
    exit 1
  fi
  read -r mkosi_commit mkosi_version <<EOF
$mkosi_ref
EOF
  jq --arg version "$mkosi_version" --arg commit "$mkosi_commit" \
    '.mkosi.version = $version | .mkosi.commit = $commit' artifact/locks/amd64.json >"$tmp_dir/amd64-with-mkosi.json"
  mv "$tmp_dir/amd64-with-mkosi.json" artifact/locks/amd64.json
fi

if changed_from_base utils/testutils/envtest/assets.go test/e2e/config/tart.yaml; then
  capi_version="$(sed -nE 's|.*cluster-api/(v[0-9]+\.[0-9]+\.[0-9]+).*|\1|p' utils/testutils/envtest/assets.go)"
  if [ -z "$capi_version" ]; then
    echo 'Cluster API envtest fixture version is missing' >&2
    exit 1
  fi

  fixture_dir="test/envtest/crds/cluster-api/$capi_version"
  if [ ! -f "$fixture_dir/cluster.x-k8s.io_clusters.yaml" ] || [ ! -f "$fixture_dir/cluster.x-k8s.io_machines.yaml" ]; then
    fixture_download_dir="$tmp_dir/cluster-api-crds"
    mkdir -p "$fixture_download_dir"
    for fixture in cluster.x-k8s.io_clusters.yaml cluster.x-k8s.io_machines.yaml; do
      curl --fail --silent --show-error --location \
        "https://raw.githubusercontent.com/kubernetes-sigs/cluster-api/$capi_version/config/crd/bases/$fixture" \
        --output "$fixture_download_dir/$fixture"
    done
    rm -rf test/envtest/crds/cluster-api
    mkdir -p "$fixture_dir"
    mv "$fixture_download_dir"/* "$fixture_dir/"
  fi

  test_version="$(sed -nE 's|.*Name:     "(v[0-9]+\.[0-9]+\.[0-9]+)".*|\1|p' test/e2e/provisioning/repository_config_test.go | head -n 1)"
  if [ -z "$test_version" ]; then
    echo 'Cluster API version is missing from repository config test' >&2
    exit 1
  fi
  sed -i.bak "s/$test_version/$capi_version/g" test/e2e/provisioning/repository_config_test.go
  rm -f test/e2e/provisioning/repository_config_test.go.bak
fi

if changed_from_base .devcontainer/devcontainer.json; then
  devcontainer upgrade --workspace-folder .
fi

git --no-pager diff --check
