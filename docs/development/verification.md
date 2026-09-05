# 検証方針

この文書は、新しいTalos専用Providerの設計・実装を検証するための正本である。検証はCIで再現できる静的確認と、実際のKubernetes/Talos/Hostが必要な受け入れ確認を分けて記録する。

## 現在の方針

Go testを全面禁止しない。実装と同時に、失敗時の影響が大きく副作用から分離できる純粋な判断、外部contract、controller再起動後の再計算へ最小限のtable test、fuzz test、契約テストを追加する。設定ファイルの存在確認やmock呼出し順だけのテストは対象にしない。test taskやCIのdefault taskへ無関係なGo testを暗黙に含めず、重要なテストtaskを明示する。

## 静的検証

静的検証とGo testを分け、次の確認をCIとローカルで共通化する。

| 対象 | 確認 |
|---|---|
| format | `mise run fmt`または同等の`gofmt` |
| 生成 | `mise run generate`でDeepCopyなどを再生成 |
| CRD/RBAC | `mise run manifests`でcontroller-genとkustomizeの結果を確認 |
| compile | `mise run build`または`go build ./...` |
| 重要な判断 | Host claim race、Retained gate、unsafe diff、resolved Secretのsemantic diff、reuse approval、quorum、configuration invariant、redacted semantic digestの必要最小限のGo test |
| 静的解析 | `mise run lint`、`go vet ./...`、必要なlint task |
| 差分 | `git --no-pager diff --check` |
| 旧設計の残存 | `rg`で`v1beta1`、`TartHostOperation`、agent、workflow、旧artifact、`internal`、`pkg`を確認 |
| secret境界 | log、Event、Status、metrics label、manifestへcredentialが含まれないことを目視確認 |

API groupを変更した場合は、Infrastructure、Bootstrap、Control PlaneのCRD group、scope、reference、aggregated RBACが一致していることを確認する。CAPI contractへ参加するCRDの`cluster.x-k8s.io/v1beta2: v1alpha1` label、`TartCluster.spec.id`、Control Planeの`spec.machineTemplate.spec.infrastructureRef`、`spec.machineTemplate.spec.deletion`、`status.versions`、scale subresource、Bootstrap Config templateの入力元、ClusterClassのSSA dry-run対応も確認する。`TartHost.spec.id`がmetadata UIDから独立し、stable identityの重複時にallocationとmaintenance applyをfail closedし、claimがSSAではなくatomic CASであることも確認する。Bootstrap Secretのtype、single `value` key、決定論的なname、label、OwnerReference、Secret-backed raw configuration、Cluster ID付きgeneration単位のcluster secretとTalos CA rotationの完了後切替、workload kubeconfig Secretの維持も契約確認の対象とする。
- `TartHost.spec.id`と`TartCluster.spec.id`がTemplateやSSA dry-runで生成されず、concrete Resourceのnon-dry-run CREATE後に一度だけ永続化される。ID確定前にbundle生成、Host claim、provisioningが開始されず、DR復元では既存IDを保持し、同名Cluster再作成では新IDになる。
- `TartCluster.spec.updatePolicy.allowDowntime`がsingle-nodeのnode-disruptive updateを許可する唯一のdesired-state契約であり、未指定または`false`なら開始しない。Tart v1alpha1に自動replacement opt-inがなく、再構築は明示的なMachine削除と`Reprovision`承認である。

生成物の検証でcontroller-genやkustomizeが必要な場合は、miseで管理したversionを使用する。toolの出力を手で修正して検証を通してはならない。

## 実装後の受け入れ確認

Go testで検証できる純粋判断と、実機、kind、envtest、契約テストで検証する外部境界を分ける。未実施の項目は完了扱いにせず、実行環境、入力、観測結果、未検証の境界を記録する。

### Fresh machine

- 最小限のHost登録からBootstrap Secretなしでsecret-free maintenance Talos bootとhardware discoveryを行い、その後Bootstrap Secretを用いたmachine configuration delivery、Talos installation、authenticated API recoveryまで進む。
- `TartHost.status`へMAC以外のsystem UUID、architecture、address、disk inventoryを観測できる。
- `TartMachine`がCAPI Infrastructure Machine contractのProviderID、addresses、provisioned、Conditionsを満たす。

### Cluster lifecycle

- single node control planeを作成し、Talos OSとKubernetesを同じMachine上で更新できる。
- HA control planeを作成し、quorumを維持したscale up/downと一台ずつのupdateを確認する。
- MachineDeploymentからworkerを作成し、既存Hostを削除せずin-place updateできる。
- CAPI ClusterClassからTartCluster、TartControlPlane、template resourceを通常のreferenceで利用できる。

### Storageと安全性

- 複数diskのHostで、Talos-native disk selector、volume、encryptionを含むconfigurationをそのまま適用できる。永続データのsentinel payloadはEPHEMERALではなくUser VolumeまたはRaw Volumeへ配置する。
- disk UUIDやLinux device pathを事前登録しなくても、maintenance Talos inventoryからdisk selectorを構築できる。ユーザーのraw configuration patchが全てimmutableな`configSecretRef`へ格納され、CRD Specへinline保存されないことを確認する。Secretには非機密configurationを含められる。
- Cilium、Longhorn、TopoLVM、kube-vipなどのadd-on専用Tart APIなしでTalos configurationとKubernetes manifestを利用できる。
- 通常のTalos/Kubernetes updateでCAPI Machine replacement、Host cleaning、disk wipeが起きない。
- unsafe change、identity mismatch、quorumを守れない操作が副作用なしで`Ready=False`と具体的なreasonになる。stable identityの同時重複作成を観測しても、両Hostへのallocationとmaintenance configuration applyが停止する。
- Machine削除時にshutdownと停止確認を行い、確認不能ならclaimを保持して`Ready=False`になり、確認後もHostが`Retained`として自動allocationされない。
- MachineSetまたはControl PlaneのMachine templateに`cluster.x-k8s.io/skip-remediation`がMachine生成前から設定され、最初に作成されたMachineにもannotationが存在する。Machine作成後の後追いannotationがないと安全にならない設計になっておらず、MHCのdelete-and-recreate remediationが既定で適用されない。Tart v1alpha1では自動replacementのopt-inを提供せず、再構築は明示的なMachine削除とRetained Hostの`Reprovision`承認で開始される。
- `TartHost.spec.id`からallocation後に決定した`tart://host/<TartHost.spec.id>`が、`TartMachine.spec.providerID`、CAPI InfraMachine、Talos kubelet、Node `spec.providerID`で一致する。Host allocationとDiscovery bootはbootstrap dataを待たず、Talos provisioningはbootstrap dataを待つため循環依存がない。management cluster復元でmetadata UIDが変わってもProviderIDを維持する。`TartCluster.spec.id`もmetadata UIDから独立して維持され、同名Clusterの再作成では新IDとなり、古いbundleやRetained HostをAdoptしない。Adoptではsame cluster ID、secret generation、Host identity、ProviderID、role/version、disk identity、control-plane etcd membershipを検証する。
- Machine deletionではCAPI Machine controllerのdrain/volume detach、Control Planeのscale-down用pre-terminate delete hook、TartMachine finalizerのshutdown/停止確認/retainedFrom/claim処理が責務どおりに分離される。WoL-onlyではTalos Shutdown受理後のendpoint消失を確認し、物理電源OFFの証明と混同しない。
- CNI未導入でもAPI serverがrequestを受け付けた時点で`controlPlaneInitialized`となり、Node Readyと混同しない。
- `CanUpdateMachineSet`、`CanUpdateMachine`、`UpdateMachine`が安全なin-place updateだけを扱い、`maxSurge: 0`、`maxUnavailable: 1`のWorker rolloutで追加Hostを要求せず、一台ずつin-place updateできる。`OnDelete` strategyを自動worker in-place updateに使用しない。Control Planeも一台ずつ更新する。multi-nodeではreboot前のdrain成功を必須とし、single-nodeではcordonとgraceful evictionを可能な範囲で試した後、`TartCluster.spec.updatePolicy.allowDowntime: true`が明示されていればavailabilityを理由に永久blockせずpersistent data preservationを優先する。未指定または`false`なら開始しない。
- Topology managed clusterとdirectly managed clusterの両方で、Talos `upgrade-k8s`が一度だけ実行される。Topologyではupgrade planとversion skewが整合していれば旧worker desired versionでも開始でき、directly managedではworker desired versionとの矛盾時に`Ready=False`、`Reason=VersionSkew`になる。Kubernetes upgradeのavailability sequencingをMachineDeploymentの`maxUnavailable`へ誤って依存しない。
- `CanUpdateMachineSet`/`CanUpdateMachine`がsafe full coverageだけをSuccess + complete patchで返し、unsafe/unknown/partial diffをpatchなしのFailureでvetoする。CAPI minorごとにMachineSet、Machine、TartHost claimが一つも作られないことを確認する。初回provisioning後のmutable Talos OS/config operationがUpdate Extension以外から実行されない。
- `CanUpdateMachineSet`/`CanUpdateMachine`がold/new双方の`configSecretRef`を解決してeffective configurationをrenderし、Secret参照名ではなくsemantic diff全体を分類する。Secretがmissing、unreadable、generation不明ならunknownとしてvetoする。
- Control Plane Providerが`CanUpdateMachine`からMachine、InfraMachine、BootstrapConfigのannotation付きspec update、Machineの`UpdateMachine` hook pendingまでをrace-free、re-entrantに遷移させる。`status.versions`、replica counts、selector、scale subresource、metadata/minReadySeconds/UpToDate propagationを確認する。
- Cluster IDを含むcluster secret bundleがgeneration単位でimmutableに作成される。CA rotation開始前にTalosの準備結果から得た新しいsecret materialで次generationの`Pending` Secretが永続化され、accepted CA追加、issuing CA切替、certificate refresh、旧CA削除の正常完了を観測してから新generationがactiveに確定する。rotation中に新MachineのprovisioningとAdoptが開始されず、Cluster存続中に過去generationがGCされないことを確認する。bundle消失または世代不明後のRetained HostがAdoptではなくReprovision専用になる。

### Recovery

- provisioning、reboot、upgrade、bootstrap API呼び出し直後にcontroller-managerを停止・再起動する。
- Resourceを手動修復せず、外部のobserved stateからreconcileが継続する。
- API callの直後に停止しても、再起動後に完了済みoperationを危険に再初期化しない。
- `clusterctl move`でclaimed Hostを暗黙に移動・解放せず、未対応として`Ready=False`と安全なreasonにする。management clusterバックアップから同じ`TartHost.spec.id`とsecret bundle generationを復元できる。`cluster.x-k8s.io/paused`中はshutdown、release、cleanを開始せず、解除後にobserved stateから再開する。

## 証跡と機密情報

受け入れ確認の証跡にはResourceのUID、Conditions、Events、safeなstructured log、Talos version、health、secret-bearing valueをredactしたconfiguration digestだけを含める。Secretの値、Talos client key、Kubernetes PKI private key、Bootstrap Data、kubeconfig、BMC password、PVC payloadを保存しない。

更新による同一性は`TartHost.spec.id`、CAPI Machine UID、TartMachine UID、stable disk identityで確認する。Pod名、Node名、resourceVersion、DHCP addressだけで同一性を判定しない。永続volume上のsentinel payloadをTalos minor update、schematic change、reboot-required configuration update、Kubernetes upgradeの前後で検証し、disk identityだけの一致をデータ保持の証拠にしない。

## テストの境界

Go testはHost claim race、`Retained` Hostの自動allocation防止、unsafe diffのreplacement防止、reuse approval世代不一致、quorum判定、configuration invariant conflict、semantic digest、Bootstrap Secret contract、cluster bootstrapのidempotencyなどへ限定する。設定ファイルの存在確認やmock呼出し順だけのテストは追加しない。Talos、実storage、reboot、rollback、drain、CAPI minorごとのreplacement不発は実機、kind、envtest、契約テストの適切な境界で検証し、どの境界を未検証か記録する。
