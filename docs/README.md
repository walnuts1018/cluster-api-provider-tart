# Tartドキュメント

TartはTalos Linux専用のCluster API Providerです。現在は新アーキテクチャの再実装中であり、利用者向けの導入手順は実用可能なE2Eが成立した時点で追加します。

## 開発者向け

- [開発者向けドキュメント](development/README.md)
- [アーキテクチャ](development/architecture.md)
- [API contract](development/api-contract.md)
- [Machine lifecycle](development/lifecycle.md)
- [Talos連携](development/talos.md)
- [セキュリティと観測性](development/security.md)
- [設計判断と完成条件](development/decisions.md)
- [Release方針](development/release.md)
- [リソースとProvisioningの流れ](development/resources-and-provisioning.md)
- [検証方針](development/verification.md)

## APIとライフサイクル

Provider APIは`v1alpha1`へリセットし、Infrastructure、Bootstrap、Control Planeをそれぞれ`infrastructure.cluster.x-k8s.io`、`bootstrap.cluster.x-k8s.io`、`controlplane.cluster.x-k8s.io`へ分けます。CAPIの現行v1beta2 contractへ適合させます。通常のTalos/Kubernetes updateは、同じCAPI Machine、`TartMachine`、`TartHost`、diskを維持するin-place updateを第一選択とし、安全に実行できない変更は`Ready=False`と具体的なreasonで停止します。`TartHost.spec.id`と`TartCluster.spec.id`はmetadata UIDから独立した永続identityであり、TemplateやSSA dry-runでは生成せず、concrete Resourceのnon-dry-run CREATE後に一度だけ確定します。通常CREATEのpreset IDは拒否し、DR復元では承認済みannotationとinfra administratorの権限境界を要求します。ProviderIDを`tart://host/<TartHost.spec.id>`から決定します。DiscoveryはBootstrap Secretなしのsecret-free maintenance bootとしてprovisioningから分離し、Machine削除後のHostは`Retained`として保持します。明示的に`Reusable`へ変更するまで自動allocationせず、cluster secret bundleはCluster ID付きgeneration単位でimmutableに管理します。CA rotationではrotation対象のCAだけを更新したPending generationをTalos公式semanticsのreconcile開始前に永続化し、正常完了を観測してからactive generationを切り替えます。node-disruptive updateのavailability緩和は`TartCluster.spec.updatePolicy.allowDowntime: true`を正本とし、data、identity、etcd、quorum安全性は緩和しません。
