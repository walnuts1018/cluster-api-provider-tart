# Ubuntu 24.04 と kubeadm の実機導入

> [!WARNING]
> この手順は開発版の検証を対象とします。初期 Provisioning は未完成であり、完了した
> Kubernetes Node を得ることは保証されません。対象 Host のディスクは消去されるため、隔離した
> Provisioning L2 と検証用ディスクだけを使用してください。

## 対象

この手順は、`amd64-uefi-ab-ubuntu-24.04-kubeadm/v1` Profile、Ubuntu 24.04、kubeadm
Kubernetes v1.36.2を対象とする。管理クラスタと対象Hostは、一般利用LANから分離した同一の
Provisioning L2へ接続する。管理クラスタnodeがProxyDHCP broadcastを送受信できない構成では、
この手順を使用しない。

対象HostはUEFI network boot、Wake-on-LAN、64 GiB以上のroot disk、x86-64-v1 CPUを必要とする。
`rootDeviceHints.deviceName`にはAgentから見える安定した`/dev/disk/by-id` pathを指定する。
disk名の推測やHostへのSSH操作は行わない。

## 事前準備

1. Provisioning L2だけへ接続された管理クラスタnodeを1台決め、labelを付ける。

```bash
kubectl label node MANAGEMENT_NODE tart.walnuts.dev/provisioning-network=true
```

2. controllerのAgent APIとAgent Artifact配信に使うDNS名を用意する。TLS証明書のSANには
そのDNS名を含める。iPXE binaryはこのCAを信頼する専用buildを使用する。公開の汎用iPXE binaryと
自己署名証明書の組合せは使用しない。

3. Ubuntu 24.04のOS ArtifactをLinux builder上で作成し、署名済みOCI Artifactとして公開する。
`artifact/locks/ubuntu-24.04-amd64.json`が固定するkubeadm、kubelet、kubectlのversionはv1.36.2である。

```bash
MISE_OFFLINE=1 mise run artifact-build-mkosi
ARTIFACT_IMAGE=dist/os-artifact/os.img \
ARTIFACT_VERITY=dist/os-artifact/os.verity \
ARTIFACT_KERNEL=dist/os-artifact/vmlinuz \
ARTIFACT_INITRD=dist/os-artifact/initrd \
ARTIFACT_VERITY_ROOT_HASH="$(tr -d '[:space:]' < dist/os-artifact/verity-root-hash)" \
ARTIFACT_SIGNING_KEY=PATH_TO_OS_ARTIFACT_PRIVATE_KEY \
ARTIFACT_SIGNING_KEY_ID=operator-os-v1 \
MISE_OFFLINE=1 mise run artifact-manifest
```

Artifactをregistryへ公開した後、出力されたdigest固定`oci://...@sha256:...`参照を保存する。
可変tagを`TartMachine`へ指定してはならない。

4. managerを実行するnode上の`/srv/tart/agent-artifact`へ検証済みAgent Artifactを配置する。
directoryには`manifest.json`、`manifest.signature.json`、`vmlinuz`、`initrd`が必要である。
Agent Artifactの署名公開鍵、OS Artifactの署名公開鍵、Agent Plan署名鍵、Agent API証明書と鍵を
operatorが管理する。

## Provider導入

次の例では、`PROVISIONING_ADDRESS`はProvisioning L2から到達できる管理nodeのDNS名またはIPである。
Secret名とConfigMap名は、`config/real-hardware`が参照する名前と完全一致させる。

```bash
export TART_NAMESPACE=cluster-api-provider-tart-system
export PROVISIONING_ADDRESS=provisioning.example.test

kubectl create namespace "$TART_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$TART_NAMESPACE" create configmap tart-provisioning-settings \
  --from-literal=bootstrapAdvertiseAddress="$PROVISIONING_ADDRESS" \
  --from-literal=agentAPIURL="https://$PROVISIONING_ADDRESS:8444" \
  --from-literal=agentArtifactBaseURL="https://$PROVISIONING_ADDRESS:8082" \
  --from-literal=agentArtifactKeyID=operator-agent-v1 \
  --from-literal=osArtifactKeyID=operator-os-v1 \
  --from-literal=agentPlanKeyID=operator-plan-v1

kubectl -n "$TART_NAMESPACE" create secret generic tart-provisioning-credentials \
  --from-file=agent-api.crt=PATH_TO_AGENT_API_CERTIFICATE \
  --from-file=agent-api.key=PATH_TO_AGENT_API_PRIVATE_KEY \
  --from-file=agent-artifact-public.pem=PATH_TO_AGENT_ARTIFACT_PUBLIC_KEY \
  --from-file=os-artifact-public.pem=PATH_TO_OS_ARTIFACT_PUBLIC_KEY \
  --from-file=agent-plan-private.pem=PATH_TO_AGENT_PLAN_PRIVATE_KEY

IMG=REGISTRY/cluster-api-provider-tart:TAG MISE_OFFLINE=1 mise run build-installer-real-hardware
kubectl apply -f dist/install-real-hardware.yaml
kubectl -n "$TART_NAMESPACE" rollout status deployment/cluster-api-provider-tart-controller-manager --timeout=5m
```

`config/real-hardware`は`hostPath`を使うため、controller Deploymentを別nodeへ移動する前に
同じAgent Artifactを移動先の`/srv/tart/agent-artifact`へ配置する。Agent Artifactのkey、CA、
OS側first-boot設定は同じ信頼束で生成する。

## Host inventory

control plane用Hostとworker用Hostを別labelで登録する。`systemUUID`、MAC address、disk serial、
WWNは実機から取得した値に置き換える。

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TartHost
metadata:
  name: cp-01
  namespace: default
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

workerには`metadata.name`、MAC、UUID、disk identityを変え、`tart.walnuts.dev/role: worker`を付ける。

## Cluster作成

`clusterctl`へこのrepositoryのInfrastructure Providerを登録し、kubeadm bootstrap/control-plane
providerと合わせて初期化する。`OS_ARTIFACT_REF`は前節で公開したdigest固定参照を指定する。

```bash
export CLUSTER_NAME=ubuntu-kubeadm
export CONTROL_PLANE_ENDPOINT_HOST=192.0.2.100
export CONTROL_PLANE_ENDPOINT_PORT=6443
export OS_ARTIFACT_REF=oci://REGISTRY/tart/ubuntu@sha256:REPLACE_WITH_DIGEST
export OS_ARTIFACT_REGISTRY=REGISTRY

cat > /tmp/clusterctl-tart.yaml <<EOF
providers:
- name: tart
  type: InfrastructureProvider
  url: file://${PWD}/dist/install-real-hardware.yaml
variables:
  CLUSTER_NAME: ${CLUSTER_NAME}
  CONTROL_PLANE_ENDPOINT_HOST: ${CONTROL_PLANE_ENDPOINT_HOST}
  CONTROL_PLANE_ENDPOINT_PORT: ${CONTROL_PLANE_ENDPOINT_PORT}
  OS_ARTIFACT_REF: ${OS_ARTIFACT_REF}
  OS_ARTIFACT_REGISTRY: ${OS_ARTIFACT_REGISTRY}
EOF

clusterctl init \
  --config /tmp/clusterctl-tart.yaml \
  --core cluster-api:v1.13.1 \
  --bootstrap kubeadm:v1.13.1 \
  --control-plane kubeadm:v1.13.1 \
  --infrastructure tart:v0.0.0

clusterctl generate cluster "$CLUSTER_NAME" \
  --from config/templates/cluster-template-kubeadm-ubuntu.yaml \
  > "$CLUSTER_NAME.yaml"
kubectl apply -f "$CLUSTER_NAME.yaml"
```

進行は次で確認する。

```bash
kubectl get cluster,machine,tartmachine,tarthost,tarthostoperation -A
kubectl describe tarthostoperation -n default active-HOST_UID_PREFIX
```

## 現在の制約

この導入経路のmanager起動、Artifact署名検証、Host割当、CAPI/KCP/CABPK object生成までは
構成として定義されている。一方、通常Provisioning Agentのboot commitには未実装が残る。
AgentはOS/Verity payloadの書込み後にEFI boot entryとState上のcontroller trustを確定し、
再起動してfirst-bootのBootstrap適用へ遷移しなければならないが、現時点の通常Agentは
`--write-payloads-only`診断で停止する。そのため、この文書のCluster作成手順は実機検証の
前提準備であり、`Node Ready`到達を成功として扱ってはならない。

通常 Agent の boot commit、EFI boot entry、State trust の引渡しが提供されるまでは、この手順を
通常運用へ使用しないでください。診断用の`--write-payloads-only`はディスクを破壊するため、
通常の導入操作として実行してはいけません。
