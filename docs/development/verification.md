# 検証方針

この文書は、新しいTalos専用Providerの設計・実装を検証するための正本である。検証はCIで再現できる静的確認と、実際のKubernetes/Talos/Hostが必要な受け入れ確認を分けて記録する。

## 現在の方針

新設計を一気に組み立てる期間は、開発速度を優先して新しいGo testを追加せず、Go testも実行しない。既存コードを削除・置換する際に、旧テストを実行して互換性を確認することもしない。test taskやCIのdefault taskへGo testを暗黙に含めない。

この方針はテスト不要という意味ではない。実装が固まり、方針を解除するときに、外部contract、Host claimの競合、in-place updateのfail closed、controller再起動後の再計算など、保守価値が高い境界へ対象を限定して追加する。

## 静的検証

Go testを使わず、次の確認をCIとローカルで共通化する。

| 対象 | 確認 |
|---|---|
| format | `mise run fmt`または同等の`gofmt` |
| 生成 | `mise run generate`でDeepCopyなどを再生成 |
| CRD/RBAC | `mise run manifests`でcontroller-genとkustomizeの結果を確認 |
| compile | `mise run build`または`go build ./...` |
| 静的解析 | `mise run lint`、`go vet ./...`、必要なlint task |
| 差分 | `git --no-pager diff --check` |
| 旧設計の残存 | `rg`で`v1beta1`、`TartHostOperation`、agent、workflow、旧artifact、`internal`、`pkg`を確認 |
| secret境界 | log、Event、Status、metrics label、manifestへcredentialが含まれないことを目視確認 |

API groupを変更した場合は、Infrastructure、Bootstrap、Control PlaneのCRD group、reference、aggregated RBACが一致していることを確認する。Bootstrap Secretのtype、single `value` key、決定論的なname、label、OwnerReference、cluster secret bundleの一度だけの生成、workload kubeconfig Secretの維持も契約確認の対象とする。

生成物の検証でcontroller-genやkustomizeが必要な場合は、miseで管理したversionを使用する。toolの出力を手で修正して検証を通してはならない。

## 実装後の受け入れ確認

Go testを追加・実行しない期間でも、次の確認項目を実機またはkindとTalosの検証環境で別途実施する。未実施の項目は完了扱いにせず、実行環境、入力、観測結果、未検証の境界を記録する。

### Fresh machine

- 最小限のHost登録からmaintenance Talos boot、hardware discovery、machine configuration delivery、Talos installation、authenticated API recoveryまで進む。
- `TartHost.status`へMAC以外のsystem UUID、architecture、address、disk inventoryを観測できる。
- `TartMachine`がCAPI Infrastructure Machine contractのProviderID、addresses、provisioned、Conditionsを満たす。

### Cluster lifecycle

- single node control planeを作成し、Talos OSとKubernetesを同じMachine上で更新できる。
- HA control planeを作成し、quorumを維持したscale up/downと一台ずつのupdateを確認する。
- MachineDeploymentからworkerを作成し、既存Hostを削除せずin-place updateできる。
- CAPI ClusterClassからTartCluster、TartControlPlane、template resourceを通常のreferenceで利用できる。

### Storageと安全性

- 複数diskのHostで、Talos-native disk selector、volume、encryptionを含むconfigurationをそのまま適用できる。
- disk UUIDやLinux device pathを事前登録しなくても、maintenance Talos inventoryからselectionを組み立てられる。
- Cilium、Longhorn、TopoLVM、kube-vipなどのadd-on専用Tart APIなしでTalos configurationとKubernetes manifestを利用できる。
- 通常のTalos/Kubernetes updateでCAPI Machine replacement、Host cleaning、disk wipeが起きない。
- unsafe change、identity mismatch、quorumを守れない操作が副作用なしでblockedになる。
- Machine削除時にshutdownと停止確認を行い、確認不能ならclaimを保持してblockedになり、確認後もHostが`Retained`として自動allocationされない。
- MHCのdelete-and-recreate remediationがlocal persistent Machineへ既定で適用されず、`cluster.x-k8s.io/skip-remediation`の運用方針が守られる。
- `TartMachine.spec.providerID`、CAPI InfraMachine、Talos kubelet、Node `spec.providerID`が一致する。
- CNI未導入でもAPI serverがrequestを受け付けた時点で`controlPlaneInitialized`となり、Node Readyと混同しない。
- `maxSurge: 0`、`maxUnavailable: 1`のrolloutで追加Hostを要求せず、一台ずつin-place updateできる。

### Recovery

- provisioning、reboot、upgrade、bootstrap API呼び出し直後にcontroller-managerを停止・再起動する。
- Resourceを手動修復せず、外部のobserved stateからreconcileが継続する。
- API callの直後に停止しても、再起動後に完了済みoperationを危険に再初期化しない。

## 証跡と機密情報

受け入れ確認の証跡にはResourceのUID、Conditions、Events、safeなstructured log、Talos version、health、configuration digestだけを含める。Secretの値、Talos client key、Kubernetes PKI private key、Bootstrap Data、kubeconfig、BMC password、PVC payloadを保存しない。

更新による同一性はCAPI Machine UID、TartMachine UID、TartHost UID、stable disk identityで確認する。Pod名、Node名、resourceVersion、DHCP addressだけで同一性を判定しない。

## 検証方針の解除条件

Go testを再開する場合は、実装と同じ変更で次を更新する。

1. この文書の現在の方針から対象と実行コマンドを分離する。
2. `gotest` skillを保留状態から有効な規約へ戻す。
3. テスト追加の対象を重要な純粋判断または外部contractへ限定する。
4. CIで再現できるtaskを定義し、ローカルだけの成功を完了根拠にしない。

再開後に最初に追加するテストは、Host claim race、`Retained` Hostの自動allocation防止、unsafe diffのreplacement防止、Bootstrap Secret contract、cluster bootstrapのidempotencyに限定する。設定ファイルの存在確認やmock呼出し順だけのテストは追加しない。
