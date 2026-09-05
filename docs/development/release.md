# Release方針

Tartは新しいTalos専用Providerへ再実装済みである。[`.github/workflows/release.yaml`](../../.github/workflows/release.yaml)がタグpushをトリガーに、controller-managerとnetboot-serverのcontainer imageのbuild・push、および`infrastructure-components.yaml`、`metadata.yaml`、`infrastructure-provider.yaml`のGitHub Releaseへの添付までを再現可能な形で自動化している。ただし、以下の「Releaseの成熟度」で示す通り、現時点のreleaseは実機E2Eによる受け入れ確認を経ていない。

## Releaseへ含めるもの（実装済み）

- Infrastructure、Bootstrap、Control Plane Providerのcontroller-manager image、およびnetboot-server(ProxyDHCP/TFTP/iPXE)のimage
- `infrastructure.cluster.x-k8s.io/v1alpha1`、`bootstrap.cluster.x-k8s.io/v1alpha1`、`controlplane.cluster.x-k8s.io/v1alpha1`のCRD、RBAC、manager/netboot-server manifest(`clusterctl init`や`InfrastructureProvider`(cluster-api-operator)でインストールしただけで両方デプロイされる)
- CAPI contractへ対応したmetadataとprovider manifest(`metadata.yaml`、`config/operator/infrastructure-provider.yaml`)
- Talos image identity、対応architecture、boot backendの前提条件

手元で同じ内容を再現するには`mise run release-manifests`(`scripts/release-manifests/run.sh`。`CONTROLLER_IMAGE`、`NETBOOT_SERVER_IMAGE`環境変数が必要)を使う。

Talos installerやboot assetはTalosの配布方式とidentityを使用し、Tart独自OS image formatを公開しない。Cilium、Longhorn、TopoLVM、kube-vipのadd-on manifestはTartのreleaseへ同梱せず、利用者が選択したKubernetes addon layerへ委譲する。

## Releaseの成熟度（実機E2Eは未実施）

現在のrelease workflowは、CIでの生成・build・vet・lint・manifest検証を通過した状態のcontainer imageとmanifestを機械的に公開するものであり、以下は**まだ実機E2Eで検証されていない**。releaseを利用する際はこの前提を理解すること。

- [対応version matrix](compatibility.md)は未定義のままであり、tested CAPI minor、Talos minor、Kubernetes version rangeの組み合わせは存在しない。
- Fresh machine(WoL/Redfish起動→PXE→Talos install→Cluster完成)、single node、HA control plane、worker、storage、recoveryの受け入れ結果。
- CAPI minorごとのunsafe diffに対するreplacement不発、Discovery、Cluster IDの復元・同名再作成分離、Talos CA rotationのPending generation永続化と完了後active切替、rotation対象外materialの保持、rollback、drain、availability理由に対する`TartCluster.spec.updatePolicy.allowDowntime`判定、storage payload保持。

実機E2Eを実施し[検証方針](verification.md)の受け入れ確認を満たした組み合わせから、[対応version matrix](compatibility.md)へ「tested」として追記していく。Workerの`OnDelete` strategyはin-place updateのtested profileへ含めない。
