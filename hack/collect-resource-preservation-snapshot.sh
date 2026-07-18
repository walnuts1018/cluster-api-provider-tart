#!/usr/bin/env bash
set -euo pipefail

namespace=${1:?検証用Namespaceを指定してください}
output=${2:?出力先のJSONを指定してください}

command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }
command -v kubectl >/dev/null || { echo "kubectl is required" >&2; exit 1; }
[[ "$namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || { echo "invalid namespace: $namespace" >&2; exit 1; }

work_dir=$(mktemp -d)
# Secretを含むAPI応答は正規化後も残さず、途中失敗時にも削除する。
trap 'rm -rf "$work_dir"' EXIT

kubectl get \
  deployments.apps,statefulsets.apps,services,configmaps,secrets,persistentvolumeclaims \
  --namespace "$namespace" \
  --output json >"$work_dir/namespaced.json"
kubectl get persistentvolumes --output json >"$work_dir/persistentvolumes.json"
kubectl version --output json >"$work_dir/version.json"

jq -e --arg namespace "$namespace" '
  [
    .items[]
    | select(
        .kind == "Deployment"
        or .kind == "StatefulSet"
        or .kind == "Service"
        or .kind == "ConfigMap"
        or .kind == "Secret"
        or .kind == "PersistentVolumeClaim"
      )
    | {
        apiVersion,
        kind,
        namespace: .metadata.namespace,
        name: .metadata.name,
        uid: .metadata.uid
      }
  ]
  | if length == 0 then error("no preservation resources found") else . end
' "$work_dir/namespaced.json" >"$work_dir/resources.json"

jq -e --arg namespace "$namespace" '
  [
    .items[]
    | select(.spec.claimRef.namespace == $namespace)
    | {
        apiVersion,
        kind,
        namespace: "",
        name: .metadata.name,
        uid: .metadata.uid
      }
  ]
' "$work_dir/persistentvolumes.json" >"$work_dir/persistentvolume-resources.json"

jq -e '
  [
    .items[]
    | select(.kind == "PersistentVolumeClaim")
    | {
        namespace: .metadata.namespace,
        name: .metadata.name,
        readerPod: .metadata.annotations["tart.infrastructure.cluster.x-k8s.io/preservation-reader-pod"],
        payloadPath: .metadata.annotations["tart.infrastructure.cluster.x-k8s.io/preservation-payload-path"]
      }
    | if (.readerPod | type) != "string" or .readerPod == "" then error("PVC preservation reader pod annotation is required") else . end
    | if (.payloadPath | type) != "string" or (.payloadPath | test("^/[A-Za-z0-9._/-]+$")) == false or (.payloadPath | contains(".."))
      then error("PVC preservation payload path annotation is invalid")
      else .
      end
  ]
  | if length == 0 then error("at least one PVC is required") else . end
' "$work_dir/namespaced.json" >"$work_dir/pvcs-to-read.json"

jq -c '.[]' "$work_dir/pvcs-to-read.json" | while IFS= read -r pvc; do
  pvc_namespace=$(jq -r '.namespace' <<<"$pvc")
  pvc_name=$(jq -r '.name' <<<"$pvc")
  reader_pod=$(jq -r '.readerPod' <<<"$pvc")
  payload_path=$(jq -r '.payloadPath' <<<"$pvc")
  digest=$(kubectl exec --namespace "$pvc_namespace" "$reader_pod" -- sha256sum "$payload_path" | awk '{print $1}')
  [[ "$digest" =~ ^[0-9a-f]{64}$ ]] || { echo "invalid PVC digest for $pvc_namespace/$pvc_name" >&2; exit 1; }
  jq -n \
    --arg namespace "$pvc_namespace" \
    --arg name "$pvc_name" \
    --arg sha256 "$digest" \
    '{namespace: $namespace, name: $name, sha256: $sha256}' >>"$work_dir/pvc-digests.jsonl"
done

jq -nS \
  --arg collected_at "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" \
  --slurpfile namespaced "$work_dir/resources.json" \
  --slurpfile persistentvolumes "$work_dir/persistentvolume-resources.json" \
  --slurpfile pvcs "$work_dir/pvc-digests.jsonl" \
  --slurpfile version "$work_dir/version.json" \
  '{
    schemaVersion: "v1alpha1",
    collectedAt: $collected_at,
    cluster: {
      kubernetesVersion: $version[0].serverVersion.gitVersion
    },
    resources: (($namespaced[0] + $persistentvolumes[0]) | sort_by(.namespace, .kind, .name)),
    pvcs: ($pvcs | sort_by(.namespace, .name))
  }' >"$work_dir/snapshot.json"

mv "$work_dir/snapshot.json" "$output"
