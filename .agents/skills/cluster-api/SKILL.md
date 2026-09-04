---
name: cluster-api
description: TartのCluster API Provider contractとResource実装を確認する
when_to_use: CAPI Resource、Provider contract、Controller、ClusterClass、Runtime Extensionを実装・レビューする時
---

# Cluster API実装方針

Cluster APIのResourceとcontrollerを変更する前に、[公式Provider documentation](https://cluster-api.sigs.k8s.io/developer/providers/overview)と、このリポジトリの[API contract](../../../docs/development/api-contract.md)を確認する。CAPI contractの仕様を推測で実装せず、依存しているCAPI versionの型とcontractを確認する。

## Versionの切り分け

TartのAPIは`infrastructure.cluster.x-k8s.io/v1alpha1`へリセットする。これはCAPI coreのversionではない。CAPI core resourceと現行contractは実装時点の`cluster.x-k8s.io/v1beta2`へ合わせる。旧Tart `v1beta1`とのconversionや互換性は作らない。

## Spec、Status、reference

- `Spec`は利用者またはCAPIが宣言したdesired state、`Status`はTart、Talos、workload clusterから観測したactual stateとConditionsだけを持つ。
- Statusの`observedGeneration`は対応するSpec generationを反映する。workflow phase、step番号、retry counterをStatusへ保存しない。
- Infrastructure Clusterはcontrol plane endpoint、provisioned、failure domains、Conditionsをcontractに従って公開する。
- Infrastructure Machineは`spec.providerID`、`status.initialization.provisioned`、`status.addresses`、failure domain、Conditionsをcontractに従って扱う。
- Bootstrap Configは`status.initialization.dataSecretCreated`、`status.dataSecretName`、Conditionsを公開し、Secret dataをStatusへコピーしない。
- Control Planeは`spec.version`、`spec.replicas`、machine template、`status.version`、`status.initialization.controlPlaneInitialized`、replica counts、selector、Conditionsをcontractに従って扱う。
- `TartHost`はCAPI Machineより長寿命なので、CAPI MachineのOwnerReferenceを設定しない。`TartMachine`とbootstrap resourceは対応するCAPI resourceとのOwnerReferenceとCAPI labelを正しく設定する。

## Controller

Reconcileは、Kubernetes desired state、TartHost observed state、Talos API observed state、必要なworkload cluster observed stateから毎回次の安全なactionを判断する。controller再起動後にprocess memoryや独自Operation resourceがなくても継続できることを確認する。

Resourceの作成とspecの管理にはserver-side applyを使う。StatusにはStatus subresourceへのserver-side applyまたはpatchを使う。field managerを責務ごとに固定し、ユーザー、CAPI core、別providerのfieldを上書きしない。`Create`や通常の`Update`でresource全体を上書きしない。

Transient errorはrequeueし、identity mismatch、unsafe storage change、quorumを守れないscale down、unsupported update pathは明確なblocked Conditionへ反映する。Machine deletionの通常処理はHost claim解除とdata保持であり、disk wipeではない。

## Provider contract

Infrastructure、Bootstrap、Control Planeの各Providerは、CAPIが読むreference、labels、OwnerReference、readiness Conditions、deletion semanticsを満たす。ClusterClassからtemplate resourceを通常のCAPI resourceとして参照できるようにし、Tart専用installation pathを要求しない。

Control Planeのreplica、Kubernetes version、etcd membershipはControl Plane Providerが所有する。Infrastructure Providerがcontrol planeのquorumやadd-onを管理しない。CNI、CSI、kube-vip、observabilityなどはTalos configurationまたはKubernetes addon layerへ委譲する。

## Runtime Extension

CAPIのin-place update hookを使う場合、`CanUpdateMachine`、`CanUpdateMachineSet`、`UpdateMachine`のrequest/response型とretry semanticsをCAPI dependencyから確認する。in-placeで安全に扱えるTalos image/config差分だけを許可し、identity、destructive storage change、判断不能な差分をMachine replacementの許可として返さない。

## Secret

Talos machine secrets、Kubernetes PKI private key、Talos client key、Bootstrap Data、kubeconfig、BMC credentialはStatus、Event、通常log、metrics labelへ出さない。Bootstrap dataはBootstrap ProviderがSecretへ格納し、Infrastructure Providerは必要なdataを取得してTalos APIへ渡すだけにする。
