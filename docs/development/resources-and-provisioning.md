# リソースとProvisioningの流れ

この文書は、TartのResource ModelをCluster APIのreference、所有関係、観測結果、外部副作用へ対応付ける。処理の進捗を保存するOperation Resourceは存在しない。

## Resourceの責務

| Resource | 寿命 | Specの正本 | Statusに保存する観測 |
| --- | --- | --- | --- |
| `TartHost` | 物理/仮想Hostの寿命 | Host identity、power/boot capability、selection条件、`reusePolicy`、controller-managed consumerRef | inventory、addresses、reachability、allocation eligibility、Conditions |
| `TartCluster` | CAPI `Cluster`の寿命 | control plane endpointとcluster-level infrastructure | endpoint反映、provisioned、failure domains、Conditions |
| `TartMachine` | CAPI `Machine`の寿命 | Host selection、Talos image identity、ProviderID、machine infrastructure identity | Host binding、Talos version、addresses、ProviderID反映、provisioned、Conditions |
| `TartMachineTemplate` | templateの寿命 | `TartMachine`のtemplate Spec | Statusなし、または標準的なConditionsのみ |
| `TartBootstrapConfig` | CAPI Machineのbootstrap dataの寿命 | user-owned Talos configuration/patches | Secret生成、configuration digest、Conditions |
| `TartBootstrapConfigTemplate` | templateの寿命 | `TartBootstrapConfig`のtemplate Spec | Statusなし、または標準的なConditionsのみ |
| `TartControlPlane` | CAPI Clusterのcontrol planeの寿命 | version、replicas、machine template、bootstrap template reference | replica counts、version、control plane initialized、kubeconfig観測、Conditions |
| `TartControlPlaneTemplate` | templateの寿命 | `TartControlPlane`のtemplate Spec | Statusなし、または標準的なConditionsのみ |

API groupはInfrastructure、Bootstrap、Control Planeで分割する。`TartMachine`はCAPI `Machine`と1対1で対応し、通常はCAPI `Machine`がownerとなる。`TartHost`はMachineより長寿命なのでMachineのOwnerReferenceを設定しない。

## 正本とcache

| 情報 | 正本 | Statusに置けるもの |
| --- | --- | --- |
| Cluster topology、replica、Kubernetes desired version | Cluster API | 現在の観測値 |
| Host inventory、controller-managed allocation binding | `TartHost`のSpecとTalos観測 | inventory、`Claimed`/`Retained`/`Reusable` Condition |
| Machineのdesired infrastructure | `TartMachine` | Talos version、ProviderID、addressesの観測 |
| Talos desired configuration | `TartBootstrapConfig`とCAPI/Tart context | configuration digest、生成済みSecret名 |
| Talos actual configuration、version、disk | Talos API | observed version、reachability、inventory |
| Kubernetes actual health | workload Kubernetes APIとTalos API | ready/available counts、Conditions |
| cluster-level Talos/Kubernetes secret material | Cluster namespaceのimmutable Secret | Secret名と存在状態 |

同じdesired stateを複数Resourceへコピーして正本にしない。Statusのdigestやversionはcacheであり、次回reconcileでは外部APIとdesired stateを再確認する。

## Host claimとallocation eligibility

Host選択は、明示的な`hostRef`があればそれを優先し、なければarchitecture、labels、hardware capability、failure domain、availabilityを満たすHostからdeterministicに選択する。選択後は`TartHost.spec.consumerRef`へ対象`TartMachine`のnamespace、name、UIDをcontroller-managed bindingとして書き込む。`TartHost.status.claimedBy`をallocation lockの正本にしない。

claimのserver-side applyではresourceVersion、UID、既存consumerを検証し、同じHostを複数Machineが利用しないことを保証する。既存claimが別Machineを指す場合に強制上書きせず、競合として扱う。`TartMachine.status.hostRef`はHost bindingの観測である。

Hostのallocation eligibilityは次のように区別する。

```text
Available
  consumerRefがなく、Retainedではなく、自動allocation可能

Claimed
  consumerRefがあり、特定のTartMachineに割り当て済み

Retained
  Machine削除後もdataやTalos identityが残るため、自動allocation不可

Reusable
  ユーザーが明示的に再利用を認め、selector候補に戻った状態
```

`Retained`はworkflow phaseではない。Machine削除後にclaimを解除する場合も、Hostを自動的に`Available`へ戻さず`Retained`へ移行する。`TartHost.spec.reusePolicy`の既定値は`Retain`であり、ユーザーが`Reusable`へ変更し、停止・identity・inventoryの安全条件を再確認できた場合だけselectorの候補へ戻す。destructiveなreprovision、clean、既存installationのadoptは通常allocationに含めず、別の明示的なlifecycleとして扱う。

## Fresh Machine

```text
CAPI Machine / TartMachine作成
        ↓
Hostをdeterministicに選択してspec.consumerRefをclaim
        ↓
power onまたはboot backendへmaintenance bootを要求
        ↓
maintenance Talos APIからidentity、address、hardware inventoryを取得
        ↓
TartHost.statusへinventoryとClaimedを反映
        ↓
Bootstrap Providerが生成したSecretからcomplete machine configurationを取得
        ↓
Talos APIへconfigurationをapply
        ↓
Talos installerがinstallationを実行
        ↓
再起動後にauthenticated Talos APIへ接続
        ↓
version、health、ProviderID、addressを観測
        ↓
TartMachineのInfrastructureReadyを反映
```

Tartはblock deviceへimageを書き込まず、partition tableを直接編集しない。installer disk、volume、encryption、kernel module、extra manifestなどはTalos-native configurationで指定し、system extensionはimage schematicで指定する。

maintenance modeからauthenticated APIへの切り替えは、固定のstep番号ではなく、到達可能なendpoint、Host identity、Talos version、configurationの観測結果で判断する。controller再起動後も同じ観測から継続できる。

## Bootstrap Secretとcluster secret

`TartBootstrapConfig`はCAPI Bootstrap contractに従うSecretを一つ生成する。Secret名は決定論的に導出して`status.dataSecretName`へ記録し、typeは`cluster.x-k8s.io/secret`、dataは単一の`value` key、cluster labelとBootstrapConfigのcontroller OwnerReferenceを設定する。`value`には対象Machineへ適用するcomplete Talos machine configurationだけを格納し、cluster bundleを独自keyへ分解しない。

Talos/Kubernetesのcluster-level PKIとsecret materialはClusterごとに一度だけ生成し、Cluster namespaceの`<cluster-name>-talos-secrets` Secretを全BootstrapConfigから参照する。初期化後にbundleが欠落しても再生成せず、`Blocked`として報告する。

Control Plane ProviderはCluster namespaceの`<cluster-name>-kubeconfig` Secretを生成・維持する。type、label、single `value` key、OwnerReferenceをCAPI contractに合わせ、client certificateの期限を観測して更新する。

## Control Planeの初回bootstrapとscale

Control Plane Providerは最初のcontrol plane Machineに対応するBootstrapConfigを、`TartControlPlane`のmachine templateとCAPI Machine contextから生成する。role、Kubernetes version、cluster endpointをBootstrapConfig Specへコピーしない。control plane labelとprovider-owned contextからeffective configurationを合成し、初回のTalos etcd bootstrapだけをControl Plane Providerが所有する。

初回bootstrap要求後にcontrollerが停止しても、次回はTalosのetcd/Kubernetes healthとCluster APIの初期化状態を先に確認し、bootstrap済みのclusterへ再初期化を送らない。`controlPlaneInitialized`はAPI serverがrequestを受け付けられる時点でtrueにし、全Node ReadyやCNI導入を待たない。

HA構成のscale upでは、既存clusterがhealthyであることを確認してから新しいMachineを作成し、Talos configuration、reachability、healthを確認する。scale downでは、対象memberがetcd quorumを壊さず、Talosがmember removalを安全に完了できることを観測してからMachine削除へ進む。安全性を判定できない場合は削除せず、`Blocked`または`UnsafeControlPlaneOperation`をConditionへ設定する。

## Update

更新は次の4種類を別々に扱う。

| 変更 | 原則 | 完了の観測 |
| --- | --- | --- |
| Talos OS version/image | 既存Machine上でTalos upgrade APIを呼ぶ | desired image、reboot後のreachability、health |
| Talos machine configuration | Talos APIが許可する場合だけapplyする | Talos actual configuration、digest、health |
| Kubernetes version | CAPI desired versionを正本にControl Plane Providerがcluster-wideにsequenceする | control plane/workerのactual versionとCAPI Conditions |
| Host identity、破壊的disk topology | 自動更新しない | `UnsafeChange`または`RequiresExplicitReprovision` |

Talosの`upgrade-k8s`後は全Nodeのactual Kubernetes versionを観測してからControl Plane Statusを更新し、その後CAPIがworkerの`Machine.spec.version`を伝播させる。worker側はactual versionが既にdesired versionなら、重複upgradeなしでUpdateMachineを完了する。

full machine configurationの再applyでは、current CAPI versionを必ず反映する。live configurationのKubernetes component image versionを古いdigestで上書きしないため、version-managed fieldをuser patchから分離する。

通常更新では同じCAPI Machine、`TartMachine`、`TartHost`、diskを維持する。CAPIのrolloutがimmutable差分からreplacementを提案する可能性がある場合も、Tartは安全に更新できない差分を`UnsafeChange`または`Blocked`として報告し、Hostの自動再利用を許可しない。

## Rolling updateとMachineHealthCheck

Tartの標準rollout profileは、対応するCAPI resourceの設定で`maxSurge: 0`、`maxUnavailable: 1`を推奨する。`maxUnavailable: 0`ではCAPIがbufferのためsurge Machineを作成し得るため、追加Hostを必要とする設定として明示する。Tartは独自のrollout controllerや`maxUnavailable`実装を作らない。

local persistent stateを保持するMachineでは、MHCのdelete-and-recreate remediationを安全な既定値とみなさない。初期運用では対象Machineへ`cluster.x-k8s.io/skip-remediation`を設定し、unhealthy Machineの自動削除を止める。将来のexternal remediationは、同じMachine、TartMachine、TartHostを維持したままpower cycle、Talos recovery、health確認を行う。

## Deletion

```text
CAPI Machine削除
        ↓
CAPIのdrain / Node lifecycle完了を待つ
        ↓
control planeならetcd member detachの安全性を確認
        ↓
authenticated Talos APIへshutdown/quiesceを要求
        ↓
Hostが停止したことを観測して確認
        ↓
TartHost.spec.consumerRefを解除
        ↓
物理dataを保持したRetained Hostとして残す
```

Talos APIに到達できない、shutdownの結果を確認できない、またはHostが停止していない場合はclaimを解放せず、finalizerを保持して`Blocked`とする。停止確認前にclaimを解放してはならない。Machine削除後も旧clusterへ接続可能なHostが動作し続けることを防ぐためである。

Machine deletionはdata retentionを意味し、disk wipe、cleaning、reprovisioningを意味しない。明示的なforce releaseやdestructive cleanを将来追加する場合も、通常updateや通常deleteから暗黙に呼び出さず、別の権限、確認、監査、Conditionを要求する。

## Conditionsとerror

Statusには`Ready`、`Claimed`、`Retained`、`Reusable`、`TalosReachable`、`Provisioned`、`UpToDate`、`Updating`、`Healthy`、`Blocked`など外部から意味を理解できるConditionだけを置く。`PreparingBoot`、`Writing`、`Verifying`のようなworkflowのprogram counterは保存しない。

電源投入待ち、DHCP address待ち、maintenance API待ち、reboot中、Kubernetes APIの一時的なunavailableは、再試行可能なConditionとrequeueとして扱う。identity mismatch、無効なdisk selector、destructive change、quorumを守れないscale down、対応していないupdate pathは、retryを続けず明確なblocked Conditionへ遷移させる。

外部API call直後にcontrollerが停止しても、再起動後はversion、reachability、health、configuration digest、ProviderID、etcd membership、Secret、Node状態を観測して未開始・実行中・完了済み・失敗を判断する。API callを送った事実だけをStatusへ保存しない。
