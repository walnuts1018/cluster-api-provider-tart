---
name: cluster-api
description: TartのCluster API Provider contractとResource実装を確認する
when_to_use: CAPI Resource、Provider contract、Controller、ClusterClass、Runtime Extensionを実装・レビューする時
---

# Cluster API実装方針

Cluster APIのResourceとcontrollerを変更する前に、[公式Provider documentation](https://cluster-api.sigs.k8s.io/developer/providers/overview)と、このリポジトリの[API contract](../../../docs/development/api-contract.md)を確認する。CAPI contractの仕様を推測で実装せず、依存しているCAPI versionの型とcontractを確認する。

## VersionとAPI group

Tart独自APIは次のgroup/versionへ分けて`v1alpha1`へリセットする。

- Infrastructure: `infrastructure.cluster.x-k8s.io/v1alpha1`
- Bootstrap: `bootstrap.cluster.x-k8s.io/v1alpha1`
- Control Plane: `controlplane.cluster.x-k8s.io/v1alpha1`

これはCAPI coreのversionではない。CAPI core resourceと現行contractは実装時点の`cluster.x-k8s.io/v1beta2`へ合わせる。旧Tart `v1beta1`とのconversionや互換性は作らない。別groupのprovider resourceをCAPI coreが参照できるようaggregated RBACを生成する。

## Spec、Status、reference

- `Spec`は利用者またはCAPIが宣言したdesired state、`Status`はTart、Talos、workload clusterから観測したactual stateとConditionsだけを持つ。
- Statusの`observedGeneration`は対応するSpec generationを反映する。workflow phase、step番号、retry counterをStatusへ保存しない。
- Infrastructure Clusterはcontrol plane endpoint、provisioned、failure domains、Conditionsをcontractに従って公開する。failure domainをallocationまで接続できない間はsurfaceしない。
- Infrastructure Machineは`spec.providerID`、`status.initialization.provisioned`、`status.addresses`、failure domain、Conditionsをcontractに従って扱う。Node `spec.providerID`と必ず一致させる。
- Bootstrap Configは`status.initialization.dataSecretCreated`、`status.dataSecretName`、Conditionsを公開し、Secret dataをStatusへコピーしない。
- Control Planeは`spec.version`、`spec.replicas`、machine template、`status.version`、`status.initialization.controlPlaneInitialized`、replica counts、selector、workload kubeconfig Secretをcontractに従って扱う。
- `controlPlaneInitialized`はAPI serverがrequestを受け付ける状態を表し、全Node ReadyやCNI導入を待たない。
- `TartHost`はCAPI Machineより長寿命なので、CAPI MachineのOwnerReferenceを設定しない。`TartMachine`とBootstrap resourceは対応するCAPI resourceとのOwnerReferenceとCAPI labelを正しく設定する。

## Host allocation

`TartHost.spec.consumerRef`をcontroller-managed desired bindingとして排他的に管理し、`TartHost.status.claimedBy`をlockの正本にしない。`TartMachine.status.hostRef`はbindingの観測である。Machine削除後のHostは`Retained`として保持し、`TartHost.spec.reusePolicy: Reusable`が明示されるまでselectorの候補に戻さない。

`Machine.spec.failureDomain`が指定されている場合はHost allocatorが一致するfailure domainを必ず選ぶ。Host停止を確認できない削除ではclaimを解除せず、finalizerを保持してblockedにする。

## Bootstrap Secretとcluster secret

Bootstrap Secretは決定論的な名前（初期実装ではBootstrapConfig名）、type `cluster.x-k8s.io/secret`、single `value` key、cluster label、BootstrapConfigのcontroller OwnerReferenceを持つ。`value`にはcomplete Talos machine configurationだけを格納する。Talos/Kubernetes cluster secret bundleはClusterごとに一度だけ生成したimmutable Secretを全BootstrapConfigで共有し、BootstrapConfigごとにgenerateしない。

Control Plane ProviderはCluster namespaceへ`<cluster-name>-kubeconfig` Secretを生成・維持する。type、label、single `value` key、TartControlPlaneのcontroller OwnerReference、client certificateの更新をCAPI contractに合わせる。

## Controller

Reconcileは、Kubernetes desired state、TartHost observed state、Talos API observed state、必要なworkload cluster observed stateから毎回次の安全なactionを判断する。controller再起動後にprocess memoryや独自Operation resourceがなくても継続できることを確認する。

Resourceの作成とspecの管理にはserver-side applyを使う。StatusにはStatus subresourceへのserver-side applyまたはpatchを使う。field managerを責務ごとに固定し、ユーザー、CAPI core、別providerのfieldを上書きしない。`Create`や通常の`Update`でresource全体を上書きしない。

Transient errorはrequeueし、identity mismatch、unsafe storage change、quorumを守れないscale down、unsupported update path、shutdown未確認のdeletionは明確なblocked Conditionへ反映する。Machine deletionの通常処理はdrain、etcd detach、Talos shutdown、停止確認、claim解除、`Retained`化であり、disk wipeではない。

## Provider contract

Infrastructure、Bootstrap、Control Planeの各Providerは、CAPIが読むreference、labels、OwnerReference、readiness Conditions、deletion semanticsを満たす。ClusterClassからtemplate resourceを通常のCAPI resourceとして参照できるようにし、Tart専用installation pathを要求しない。

Control Planeのreplica、Kubernetes version、etcd membership、workload kubeconfigはControl Plane Providerが所有する。Infrastructure Providerがcontrol planeのquorumやadd-onを管理しない。CNI、CSI、kube-vip、observabilityなどはTalos configurationまたはKubernetes addon layerへ委譲する。

## Runtime Extensionとrollout

CAPIのin-place update hookを使う場合、management clusterで`RuntimeSDK=true`と`InPlaceUpdates=true`を有効にし、HTTPS endpointを`ExtensionConfig`へ登録する。TLS Secret、server certificate、必要なCA trustを管理する。現行CAPIではin-place update hookへ登録できるextensionは1つなので、deployment前に他extensionとの競合がないことを確認する。

`CanUpdateMachine`、`CanUpdateMachineSet`は安全なin-place差分だけを許可し、`UpdateMachine`は同じMachineへTalos operationを適用する。CAPIがhook未対応差分をimmutable rolloutへfallbackし得るため、hookだけでreplacementを禁止したとみなさない。`TartMachine`のblocked判定、Host Retained gate、MHC skip-remediation policyを併用する。rolloutの標準profileは対応するCAPI設定で`maxSurge: 0`、`maxUnavailable: 1`とし、Tart独自のrollout controllerは作らない。

## SecretとMHC

Talos machine secrets、Kubernetes PKI private key、Talos client key、Bootstrap Data、kubeconfig、BMC credentialはStatus、Event、通常log、metrics labelへ出さない。Bootstrap dataはBootstrap ProviderがSecretへ格納し、Infrastructure Providerは必要なdataを取得してTalos APIへ渡すだけにする。

local persistent stateを持つMachineではMHCのdelete-and-recreate remediationを既定で許可しない。初期運用では対象Machineへ`cluster.x-k8s.io/skip-remediation`を設定し、将来のexternal remediationは同じMachineを維持するpower cycle/Talos recovery方式とする。
