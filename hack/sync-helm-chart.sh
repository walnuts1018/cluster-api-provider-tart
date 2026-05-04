#!/usr/bin/env bash
set -euo pipefail

# Sync CRDs with labels from config/crd to charts/cluster-api-provider-tart/crd/crd.yaml
CHART_CRD_DIR="charts/cluster-api-provider-tart/crd"
CRD_FILE="${CHART_CRD_DIR}/crd.yaml"
KUSTOMIZE_DIR="config/crd"

# Remove old CRD bases directory if it exists
if [ -d "${CHART_CRD_DIR}/bases" ]; then
  rm -rf "${CHART_CRD_DIR}/bases"
fi

# Remove old templates/crd.yaml if it exists
if [ -f "${CHART_CRD_DIR}/templates/crd.yaml" ]; then
  rm -rf "${CHART_CRD_DIR}/templates"
fi

# Run kustomize build and output to the Helm chart CRD directory
kustomize build "$KUSTOMIZE_DIR" > "$CRD_FILE"

echo "Generated CRD with labels at ${CRD_FILE}"
