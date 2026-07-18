# 実機へのインストールと最初のクラスタ作成

この手順は、公開済みReleaseのProvider image、iPXE、OS Artifact、Provisioning Agent Artifactを利用して、Ubuntu 24.04 + kubeadmのamd64 UEFI物理ホストをCluster API (CAPI)から作成するための導入手順です。利用者はProviderをビルドしません。

## 構成と前提

- 管理クラスタ: Kubernetes v1.35以降。`kubectl` が接続済みであること
- 管理クラスタの1 node: Provisioning L2へ接続し、Provider Podを `hostNetwork` で実行できること
- 対象PC: amd64、UEFI、UEFI network boot、Wake-on-LAN、64 GiB以上のdisk、安定したdisk by-id path
- Provisioning L2: 管理node、対象PC、必要なネットワーク機器だけが参加する隔離L2。通常LANからroutingしない
- 作業者端末: `kubectl`、`kustomize`、`clusterctl`、`helm`

ProviderはDHCPでIPを配布しません。既存DHCPが対象PCへIPを配布し、ProviderはProxyDHCP/iPXE、TFTP、Agent APIを担当します。

## 1. Release成果物を選ぶ

GitHubのReleaseから、同じRelease tagの次のファイルを取得します。

- `infrastructure-components.yaml`: Provider imageとiPXEをdigest固定で参照するCAPI install manifest
- `metadata.yaml`: `clusterctl`用Provider metadata
- `reference.txt`: Ubuntu 24.04 amd64 OS Artifactのdigest固定OCI参照
- Agent Artifactの`manifest.json`、`manifest.signature.json`、`vmlinuz`、`initrd`、`agent-artifact-public.pem`、`references.env`

OS ArtifactとAgent Artifactのworkflowは、Release作成または`workflow_dispatch`で実行されます。Release以外で手動実行する場合は、指定したtagのGHCR参照を使用してください。

Provider image、iPXE、Artifactのビルド、署名、GHCRへの公開はGitHub Actionsが行います。ローカル端末で`docker build`、`mise run artifact-build-mkosi`、`mise run agent-artifact-build-real`を実行する必要はありません。

## 2. Providerを管理クラスタへインストールする

まずCAPI core、CABPK、KCPを`clusterctl`で導入し、Release assetのmanifestを適用します。

```bash
clusterctl init \
  --core cluster-api:v1.13.1 \
  --bootstrap kubeadm:v1.13.1 \
  --control-plane kubeadm:v1.13.1

export TART_NAMESPACE=cluster-api-provider-tart-system
kubectl create namespace "$TART_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$TART_NAMESPACE" create configmap tart-provisioning-settings \
  --from-literal=bootstrapAdvertiseAddress=provisioning.example.test \
  --from-literal=agentAPIURL=https://provisioning.example.test:8444 \
  --from-literal=agentArtifactBaseURL=https://provisioning.example.test:8082 \
  --from-literal=agentArtifactKeyID=release-agent-v1 \
  --from-literal=osArtifactKeyID=release-v1 \
  --from-literal=agentPlanKeyID=release-plan-v1
kubectl -n "$TART_NAMESPACE" create secret generic tart-provisioning-credentials \
  --from-file=agent-api.crt=/secure/agent-api.crt \
  --from-file=agent-api.key=/secure/agent-api.key \
  --from-file=agent-artifact-public.pem=/secure/agent-artifact-public.pem \
  --from-file=os-artifact-public.pem=/secure/os-artifact-public.pem \
  --from-file=agent-plan-private.pem=/secure/agent-plan-private.pem
kubectl apply -f infrastructure-components.yaml
kubectl -n "$TART_NAMESPACE" rollout status deployment/cluster-api-provider-tart-controller-manager --timeout=5m
```

管理nodeへProvisioning network labelを付けます。Deploymentが別nodeへ移動する場合、そのnodeにも同じAgent Artifactを先に配置してください。

```bash
kubectl label node MANAGEMENT_NODE tart.walnuts.dev/provisioning-network=true
```

Providerが起動し、Agent Artifactの署名検証が成功していることを確認します。

```bash
kubectl -n "$TART_NAMESPACE" logs deployment/cluster-api-provider-tart-controller-manager -c manager
kubectl get crd tartclusters.infrastructure.cluster.x-k8s.io
```

Agent Artifactの公開鍵とOS Artifactの公開鍵はRelease assetから取得し、Agent API証明書・秘密鍵とAgent Plan秘密鍵は管理クラスタのSecretへ登録します。Artifact payloadは、Provider Podが動くnodeの`/srv/tart/agent-artifact`へ配置します。これはビルドではなく、Release assetの展開です。

Releaseの署名を継続利用する場合は、GitHub Actionsの`OS_ARTIFACT_SIGNING_KEY`をRepository Secretとして固定してください。未設定時はworkflow実行ごとに一時鍵が生成されるため、対応する公開鍵を毎回Secretへ更新する必要があります。

```bash
mkdir -p /srv/tart/agent-artifact
cp manifest.json manifest.signature.json vmlinuz initrd /srv/tart/agent-artifact/
```

## 3. Host inventoryを登録する

対象PCごとに、実機で確認したUUID、boot MAC、disk serial/WWNを指定します。値を推測しないでください。

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TartHost
metadata:
  name: cp-01
  labels:
    tart.walnuts.dev/role: control-plane
spec:
  identifiers:
    systemUUID: REPLACE_WITH_SYSTEM_UUID
    bootMACAddress: "00:11:22:33:44:55"
  architecture: amd64
  firmware: UEFI
  platformProfile: amd64-uefi-ab-ubuntu-24.04-kubeadm/v1
  rootDeviceHints:
    deviceName: /dev/disk/by-id/REPLACE_WITH_STABLE_DISK_ID
    serialNumber: REPLACE_WITH_DISK_SERIAL
    minSizeBytes: 68719476736
  management:
    powerDriver: wol
    bootDriver: ipxe
```

control plane用とworker用を必要台数登録し、`status.phase=Available`になることを確認します。

## 4. workload clusterを作成する

`config/templates/cluster-template-kubeadm-ubuntu.yaml` をコピーし、次の値を置換します。

- `CLUSTER_NAME`
- `CONTROL_PLANE_ENDPOINT_HOST`（対象PCから到達できる固定アドレスまたはDNS）
- `OS_ARTIFACT_REF`（digest固定OCI参照）
- `OS_ARTIFACT_REGISTRY`
- `CONTROL_PLANE_MACHINE_COUNT` と `WORKER_MACHINE_COUNT`

```bash
export CLUSTER_NAME=ubuntu-kubeadm
export CONTROL_PLANE_ENDPOINT_HOST=192.168.100.100
export OS_ARTIFACT_REF=oci://registry.example.test/tart/ubuntu@sha256:REPLACE_WITH_DIGEST
export OS_ARTIFACT_REGISTRY=registry.example.test
export CONTROL_PLANE_MACHINE_COUNT=1
export WORKER_MACHINE_COUNT=1
export KUBERNETES_VERSION=v1.36.2
export CONTROL_PLANE_ENDPOINT_PORT=6443
export POD_CIDR=192.168.0.0/16
export SERVICE_CIDR=10.128.0.0/12
export PLATFORM_PROFILE=amd64-uefi-ab-ubuntu-24.04-kubeadm/v1
export CNI_URL=https://raw.githubusercontent.com/projectcalico/calico/v3.32.0/manifests/calico.yaml

envsubst < config/templates/cluster-template-kubeadm-ubuntu.yaml > "$CLUSTER_NAME.yaml"
kubectl apply -f "$CLUSTER_NAME.yaml"
```

確認コマンド:

```bash
kubectl get cluster,machine,tartcluster,tartmachine,tarthost,tarthostoperation -A -w
kubectl describe tartmachine -A
kubectl describe tarthostoperation -A
```

成功条件は、各 `TartHostOperation` が `Succeeded`、各 `TartMachine.status.initialization.provisioned=true`、対応するworkload Nodeが `Ready=True`、providerIDが一致することです。Bootstrap失敗時はpayloadを削除せずOperationが `Failed` になるため、最初にOperationとcontroller logを確認します。

## 現時点の制約

実機のUEFI、PXE、L2 broadcast、WoL、NIC driver、disk layoutはこの環境から検証できません。上記のコード経路とCI/QEMU契約は検証できますが、初回実機では必ず消去対象diskを専用にし、consoleを記録してください。`WipeAll` とProvisionは対象diskのpartitionを破壊します。

関連する詳細は [Ubuntu 24.04 kubeadm実機導入](docs/installation/ubuntu-kubeadm.md)、[real-hardware overlay](config/real-hardware/README.md)、[Task 07](docs/redesign/tasks/07-initial-provisioning.md) を参照してください。
