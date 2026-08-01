#!/usr/bin/env bash
# Provisioning E2E向けProvider installerを生成する。
set -euo pipefail
IMG="${IMG:-registry.test.walnuts.dev/cluster-api-provider-tart:e2e}"
mise run prepare-provisioning-e2e-artifact
agent_hostpath="${PROVISIONING_E2E_AGENT_ARTIFACT_HOSTPATH:-/var/lib/tart-provisioning-e2e/agent-artifact}"
sudo rm -rf "$agent_hostpath"
sudo mkdir -p "$agent_hostpath"
sudo cp -R config/provisioning-e2e/generated/agent-artifact/. "$agent_hostpath/"
sudo chmod -R a+rX "$agent_hostpath"
mise run manifests
mise run generate
manager_kustomization="$(mktemp)"
cp config/manager/kustomization.yaml "$manager_kustomization"
trap 'cp "$manager_kustomization" config/manager/kustomization.yaml; rm -f "$manager_kustomization"' EXIT
mkdir -p dist
(cd config/manager && kustomize edit set image controller="$IMG")
kustomize build config/provisioning-e2e > dist/install-e2e.yaml
