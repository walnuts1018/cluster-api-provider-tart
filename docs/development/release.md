# Release方針

Tartは新しいTalos専用Providerへ再実装中です。旧Provisioning Agent、Ubuntu/kubeadm用OS artifact、iPXE artifact、独自署名鍵を生成・公開していたRelease workflowは削除しました。実装が未完成の状態で、利用者が適用できるrelease manifestを生成しないためです。

## Releaseへ含めるもの

実用可能なreleaseでは、少なくとも次を同じcommitから再現可能な形で提供する。

- Infrastructure、Bootstrap、Control Plane Providerのcontroller-manager image
- `infrastructure.cluster.x-k8s.io/v1alpha1`、`bootstrap.cluster.x-k8s.io/v1alpha1`、`controlplane.cluster.x-k8s.io/v1alpha1`のCRD、RBAC、manager manifest
- CAPI contractへ対応したmetadataとprovider manifest
- Talos image identity、対応architecture、boot backendの前提条件
- [対応version matrix](compatibility.md)に記載した、tested CAPI minor、Talos minor、Kubernetes version range、in-place updateの範囲
- Fresh machine、single node、HA control plane、worker、storage、recovery、safetyの受け入れ結果

Talos installerやboot assetはTalosの配布方式とidentityを使用し、Tart独自OS image formatを公開しない。Cilium、Longhorn、TopoLVM、kube-vipのadd-on manifestはTartのreleaseへ同梱せず、利用者が選択したKubernetes addon layerへ委譲する。

## Release前のゲート

Release workflowを再追加する前に、[検証方針](verification.md)の受け入れ確認を満たし、CIで生成、build、vet、lint、manifest検証、重要なGo testを再現できることを確認する。CAPI minorごとのunsafe diffに対するreplacement不発、Discovery、Cluster IDの復元・同名再作成分離、Talos CA rotationのPending generation永続化と完了後active切替、rollback、drain、single-nodeの`TartCluster.spec.updatePolicy.allowDowntime`判定、storage payload保持をrelease gateへ含める。Workerの`OnDelete` strategyをin-place updateのtested profileへ含めない。
