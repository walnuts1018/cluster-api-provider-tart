#!/usr/bin/env bash
set -euo pipefail

# Sync CRD bases from config/crd/bases to charts/cluster-api-provider-tart/crd/bases
CHART_CRD_DIR="charts/cluster-api-provider-tart/crd/bases"
CONFIG_CRD_DIR="config/crd/bases"

mkdir -p "$CHART_CRD_DIR"

# Remove any CRDs in chart that no longer exist in config
for f in "$CHART_CRD_DIR"/*; do
  [ -f "$f" ] || continue
  basename_f=$(basename "$f")
  if [ ! -f "${CONFIG_CRD_DIR}/${basename_f}" ]; then
    rm "$f"
  fi
done

# Copy or update CRDs from config to chart
for f in "$CONFIG_CRD_DIR"/*; do
  [ -f "$f" ] || continue
  cp -f "$f" "$CHART_CRD_DIR/"
done

echo "Synced CRDs from ${CONFIG_CRD_DIR} to ${CHART_CRD_DIR}"
