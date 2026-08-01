#!/usr/bin/env bash
# 実機向けProvider installerを生成する。
set -euo pipefail
IMG="${IMG:-controller:latest}"
: "${AGENT_ARTIFACT_REF:?AGENT_ARTIFACT_REF is required (e.g. ghcr.io/walnuts1018/cluster-api-provider-tart-provisioning-agent:<release-tag>)}"
: "${AGENT_ARTIFACT_PUBLIC_KEY_PEM:?AGENT_ARTIFACT_PUBLIC_KEY_PEM is required}"
: "${OS_ARTIFACT_PUBLIC_KEY_PEM:?OS_ARTIFACT_PUBLIC_KEY_PEM is required}"
agent_artifact_repository="ghcr.io/walnuts1018/cluster-api-provider-tart-provisioning-agent"
case "$AGENT_ARTIFACT_REF" in
  "$agent_artifact_repository":*) agent_artifact_tag="${AGENT_ARTIFACT_REF#*:}" ;;
  *) echo "AGENT_ARTIFACT_REF must use $agent_artifact_repository:<release-tag>" >&2; exit 1 ;;
esac
case "$agent_artifact_tag" in
  ''|dev|latest) echo "AGENT_ARTIFACT_REF must use a GitHub release tag" >&2; exit 1 ;;
esac
if ! echo "$agent_artifact_tag" | grep -qE '^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$'; then
  echo "AGENT_ARTIFACT_REF has an invalid release tag" >&2
  exit 1
fi
mise run manifests
mise run generate
manager_kustomization="$(mktemp)"
cp config/manager/kustomization.yaml "$manager_kustomization"
trap 'cp "$manager_kustomization" config/manager/kustomization.yaml; rm -f "$manager_kustomization" config/real-hardware/generated/agent-artifact-patch.yaml config/real-hardware/generated/agent-artifact-public.pem config/real-hardware/generated/os-artifact-public.pem' EXIT
mkdir -p dist
mkdir -p config/real-hardware/generated
printf '%s' "$AGENT_ARTIFACT_PUBLIC_KEY_PEM" > config/real-hardware/generated/agent-artifact-public.pem
printf '%s' "$OS_ARTIFACT_PUBLIC_KEY_PEM" > config/real-hardware/generated/os-artifact-public.pem
printf '%s\n' \
  'apiVersion: apps/v1' \
  'kind: Deployment' \
  'metadata:' \
  '  name: controller-manager' \
  '  namespace: system' \
  'spec:' \
  '  template:' \
  '    spec:' \
  '      containers:' \
  '      - name: manager' \
  '        volumeMounts:' \
  '        - name: agent-artifact' \
  '          mountPath: /var/lib/tart/agent-artifact' \
  '          readOnly: true' \
  '      volumes:' \
  '      - name: agent-artifact' \
  '        image:' \
  "          reference: $AGENT_ARTIFACT_REF" \
  '          pullPolicy: IfNotPresent' \
  > config/real-hardware/generated/agent-artifact-patch.yaml
(cd config/real-hardware && kustomize edit set image controller="$IMG")
kustomize build config/real-hardware > dist/install-real-hardware.yaml
