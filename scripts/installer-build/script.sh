#!/usr/bin/env bash
# ローカル利用向けのProvider installerを生成する。
set -euo pipefail
: "${IMG:?IMG is required}"
IPXE_IMG="${IPXE_IMG:-ghcr.io/walnuts1018/cluster-api-provider-tart-ipxe}"
IPXE_TAG="${IPXE_TAG:-}"
IPXE_REF="${IPXE_REF:-}"
image_without_digest="${IMG%%@*}"
last_image_segment="${image_without_digest##*/}"
if [ -z "$IPXE_REF" ] && [ "$image_without_digest" = "$IMG" ] && [ "${last_image_segment#*:}" != "$last_image_segment" ]; then
  IPXE_TAG="${last_image_segment##*:}"
fi
if [ -z "$IPXE_REF" ]; then
  : "${IPXE_TAG:?IPXE_REF or IPXE_TAG is required when IMG is digest-pinned}"
  IPXE_REF="$IPXE_IMG:$IPXE_TAG"
fi
mise run manifests
mise run generate
manager_kustomization="$(mktemp)"
cp config/manager/kustomization.yaml "$manager_kustomization"
trap 'cp "$manager_kustomization" config/manager/kustomization.yaml; rm -f "$manager_kustomization"' EXIT
mkdir -p dist
(cd config/manager && kustomize edit set image controller="$IMG" "ghcr.io/walnuts1018/cluster-api-provider-tart-ipxe=$IPXE_REF")
kustomize build config/default > dist/install.yaml
