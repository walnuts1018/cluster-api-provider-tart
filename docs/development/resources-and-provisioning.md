# リソースとProvisioningの流れ

この文書は、TartのResource ModelをCluster APIのreference、所有関係、観測結果、外部副作用へ対応付ける。処理の進捗を保存するOperation Resourceは存在しない。

## Resourceの責務

| Resource | 寿命 | Specの正本 | Statusに保存する観測 |
| --- | --- | --- | --- |
| `TartHost` | management cluster全体の物理/仮想Hostの寿命 | immutableな`id`、Host identity、power/boot capability、selection条件、`reusePolicy`、`reuseMode`、`reuseApproval`、controller-managed consumerRef、retention record | inventory、addresses、reachability、allocation eligibility、Conditions |
| `TartCluster` | CAPI `Cluster`の寿命 | immutableなcluster ID、cluster-level infrastructure、`updatePolicy.allowDowntime` | provisioned、failure domains、active secret generation、Conditions |
| `TartMachine` | CAPI `Machine`の寿命 | Host selection、Talos image identity、ProviderID、machine infrastructure identity | Host binding、Talos version、addresses、ProviderID反映、provisioned、Conditions |
| `TartMachineTemplate` | templateの寿命 | `TartMachine`のtemplate Spec | Statusなし、または標準的なConditionsのみ |
| `TartBootstrapConfig` | CAPI Machineのbootstrap dataの寿命 | user-owned Talos configuration/patches（全てimmutableな`configSecretRef`） | Secret生成、configuration digest、Conditions |
| `TartBootstrapConfigTemplate` | templateの寿命 | `TartBootstrapConfig`のtemplate Spec | Statusなし、または標準的なConditionsのみ |
| `TartControlPlane` | CAPI Clusterのcontrol planeの寿命 | version、replicas、machine template、bootstrap template reference | replica counts、`status.versions`、control plane initialized、kubeconfig観測、Conditions |
| `TartControlPlaneTemplate` | templateの寿命 | `TartControlPlane`のtemplate Spec | Statusなし、または標準的なConditionsのみ |

API groupはInfrastructure、Bootstrap、Control Planeで分割する。CAPI contractへ参加するResourceはnamespace-scopedで、`TartHost`だけはmanagement cluster全体の物理inventoryとしてcluster-scopedにする。`TartMachine`はCAPI `Machine`と1対1で対応し、通常はCAPI `Machine`がownerとなる。`TartHost`はMachineより長寿命なのでMachineのOwnerReferenceを設定しない。

## 正本とcache

| 情報 | 正本 | Statusに置けるもの |
| --- | --- | --- |
| Cluster topology、replica、Kubernetes desired version | Cluster API | 現在の観測値 |
| Host inventory、controller-managed allocation binding | `TartHost`のSpecとTalos観測 | inventory、`Claimed`/`Retained`/`Reusable` Condition |
| Machineのdesired infrastructure | `TartMachine` | Talos version、ProviderID、addressesの観測 |
| Talos desired configuration | `TartBootstrapConfig`とCAPI/Tart context | secret値をredactしたconfiguration digest、生成済みSecret名 |
| Talos actual configuration、version、disk | Talos API | observed version、reachability、inventory |
| Kubernetes actual health | workload Kubernetes APIとTalos API | ready/available counts、Conditions |
| cluster-level Talos/Kubernetes secret material | Cluster IDを含むgeneration単位のimmutable Secretとactive generationの永続参照 | Secret名、Cluster ID、generation、active状態と存在状態 |

同じdesired stateを複数Resourceへコピーして正本にしない。Statusのdigestやversionはcacheであり、次回reconcileでは外部APIとdesired stateを再確認する。

## Host claimとallocation eligibility

Host選択は、明示的な`hostRef`があればそれを優先し、なければarchitecture、labels、hardware capability、failure domain、availabilityを満たすHostからdeterministicに選択する。選択後は`TartHost.spec.consumerRef`へ対象`TartMachine`のnamespace、name、UIDをcontroller-managed bindingとしてatomic CASで書き込む。`TartHost.status.claimedBy`をallocation lockの正本にしない。stable identityの重複はadmission webhookの全体list検査で絶対的に防止せず、controllerが重複を観測したら関係する全Hostを`Ready=False`、`Reason=IdentityConflict`としてallocationとmaintenance configuration applyから除外する。

claimは`GET → consumerRefがnilまたは自分のUIDであることを確認 → 取得したresourceVersion付きUpdate`、またはJSON Patchの`test`でcompare-and-swapする。SSAのfield ownershipをlockとして使わず、同じHostを複数Machineが利用しないことをresourceVersion競合で保証する。既存claimが別Machineを指す場合に強制上書きせず、競合として扱う。`TartMachine.status.hostRef`はHost bindingの観測である。

Hostのallocation eligibilityは次のように区別する。freshなHostは`consumerRef`と`retainedFrom`がなく、過去のMachine由来のretention記録がないため`Available`である。

```text
Available
  consumerRefとretainedFromがなく、自動allocation可能

Claimed
  consumerRefがあり、特定のTartMachineに割り当て済み

Retained
  Machine削除後にretainedFromが残り、dataやTalos identityが残るため、自動allocation不可

Reusable
  現在のretainedFromに一致する再利用承認とAdopt/Reprovision modeがあり、selector候補に戻った状態
```

`Retained`はworkflow phaseではない。Machine削除時にcontroller-managedな`TartHost.spec.retainedFrom`へ直前のconsumerと`TartCluster.spec.id`由来のcluster IDを記録し、claimを解除してもHostを自動的に`Available`へ戻さない。`TartHost.spec.reusePolicy: Reusable`、現在の`retainedFrom`に一致する`spec.reuseApproval.retainedFromUID`、`spec.reuseMode: Adopt|Reprovision`がユーザーによって明示され、停止・identity・inventoryの安全条件を再確認できた場合だけ`Reusable`へ移行する。再利用承認はSpecから消費せず、次のMachine削除で`retainedFrom.uid`が変わると無効になる。再利用指定をClaim中またはfreshな時点で設定しても、将来の削除を事前承認したことにはならない。`Adopt`はsame cluster ID、same secret generation、same Host identity、same ProviderID、compatible role/version、expected disk identityを確認してdataを保持し、control-planeではetcd membershipも検証する。`Reprovision`はdata破棄を明示的に承認する別lifecycleであり、通常allocationやupdateのfallbackではない。Cluster secret bundleが失われたRetained Hostは`Adopt`不可、`Reprovision`専用である。

## Fresh Machine

```text
CAPI Machine / TartMachine作成
        ↓
Hostをdeterministicに選択してspec.consumerRefをatomic CASでclaim
        ↓
`TartHost.spec.id`由来のProviderIDをTartMachine/InfraMachineへ設定
        ↓
bootstrap dataを待たずDiscovery bootでmaintenance Talos APIへ接続
        ↓
identity、address、hardware inventoryを取得
        ↓
TartHost.statusへinventoryとClaimedを反映
        ↓
bootstrap dataを待ち、Bootstrap Providerが生成したSecretからcomplete machine configurationを取得
        ↓
configuration apply、install用power操作を開始
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

`TartBootstrapConfig`はCAPI Bootstrap contractに従うSecretを一つ生成する。Secret名は対応するBootstrapConfig名から決定論的に導出して`status.dataSecretName`へ記録し、typeは`cluster.x-k8s.io/secret`、dataは単一の`value` key、cluster labelとBootstrapConfigのcontroller OwnerReferenceを設定する。`value`には対象Machineへ適用するcomplete Talos machine configurationだけを格納し、cluster bundleを独自keyへ分解しない。生成後のBootstrap Secretは書き換えず、mutableな変更はUpdate Extensionへ委譲する。

TartControlPlane Providerが、control-plane MachineやBootstrap Secretより先に、Talos/Kubernetesのcluster-level PKIとsecret materialをgeneration単位で生成する。Bootstrap ProviderはCluster namespaceの`<cluster-name>-talos-secrets-<cluster-id>-g<generation>` Secretをread-onlyで参照し、個別のbundle生成、更新、再生成を行わない。各Secretはimmutableで、Cluster ID labelとactive generationの永続参照を持つ。CA rotationではgeneration Nを基にrotation対象のCAだけを更新した完全なgeneration N+1を`Pending`として先に永続化する。その後、Talos公式のaccepted CA追加、issuing CA切替、certificate refresh、旧CA削除のsemanticsをmachine configuration/APIでreconcileする。自動`rotate-ca`を完了後のmaterial回収として扱わず、Pending bundleとobserved stateから再開する。rotation対象外のetcd CA、service account keyなどは変更しない。正常完了を観測してから新generationへ切り替える。rotation中は新しいMachineのprovisioningとAdoptを開始しない。Cluster存続中は過去generationをGCせず、Cluster deletion時に全Managed Machineのshutdownとretention、バックアップ保持方針、Retained Hostの再利用制約を確認した後だけGCを許可する。`TartCluster`のdeletion finalizerまたは同等の削除ゲートは、全Managed Machineのshutdownとretention完了までbundleのGCを阻止する。初期化後に必要なbundleが欠落または世代不明でも再生成せず、`Ready=False`、`Reason=SecretBundleUnavailable`として報告する。bundle消失後のRetained Hostは`Reprovision`専用である。

Control Plane ProviderはCluster namespaceの`<cluster-name>-kubeconfig` Secretを生成・維持する。type、label、single `value` key、OwnerReferenceをCAPI contractに合わせ、client certificateの期限を観測して更新する。

## Control Planeの初回bootstrapとscale

Control Plane Providerは`TartControlPlane.spec.machineTemplate.spec.infrastructureRef`とprovider-specificな`spec.bootstrapConfigTemplate`を使い、各control-plane CAPI Machineに対応する`TartMachine`と`TartBootstrapConfig`を一対一で作成する。CAPI Machineの`spec.infrastructureRef`には`TartMachine`を、`spec.bootstrap.configRef`には生成した`TartBootstrapConfig`を設定する。role、Kubernetes version、cluster endpointをBootstrapConfig Specへコピーしない。control plane labelとprovider-owned contextからeffective configurationを合成し、通常のTalos `controlplane` configurationを全control planeへ出す。初回のTalos `Bootstrap` RPCだけをControl Plane Providerが所有し、deprecatedな`init` machine typeは使用しない。

初回bootstrap要求後にcontrollerが停止しても、次回はTalosのetcd/Kubernetes healthとCluster APIの初期化状態を先に確認し、bootstrap済みのclusterへ再初期化を送らない。`controlPlaneInitialized`はAPI serverがrequestを受け付けられる時点でtrueにし、全Node ReadyやCNI導入を待たない。

HA構成のscale upでは、既存clusterがhealthyであることを確認してから新しいMachineを作成し、Talos configuration、reachability、healthを確認する。scale downでは、対象memberがetcd quorumを壊さず、Talosがmember removalを安全に完了できることを観測してからMachine削除へ進む。安全性を判定できない場合は削除せず、`Ready=False`、`Reason=UnsafeControlPlaneOperation`を設定する。

## Update

更新は次の4種類を別々に扱う。

| 変更 | 原則 | 完了の観測 |
| --- | --- | --- |
| Talos OS version/image | 既存Machine上でTalos upgrade APIを呼ぶ | desired image、reboot後のreachability、health |
| Talos machine configuration | Talos APIが許可する場合だけapplyする | Talos actual configuration、digest、health |
| Kubernetes version | CAPI desired versionを正本にControl Plane Providerがcluster-wideにsequenceする | control plane/workerのactual versionとCAPI Conditions |
| Host identity、破壊的disk topology | 自動更新しない | `UnsafeChange`または`RequiresExplicitReprovision` |

Topology managed clusterでは、CAPI upgrade planのcontrol-plane/worker stepとversion skewが目標versionに整合していれば、現在のworker `Machine.spec.version`が旧versionでもTalosの`upgrade-k8s`を開始できる。Talosの`upgrade-k8s`後は全Nodeのactual Kubernetes versionを観測してからControl Plane Statusの`status.versions`を更新し、その後CAPIがworkerの`Machine.spec.version`を伝播させる。Kubernetes upgrade自体はcluster-wide operationなので、MachineDeploymentの`maxUnavailable`でTalos operationのavailability sequencingを制御しない。worker側はactual versionが既にdesired versionなら、重複upgradeなしでUpdateMachineを完了する。

directly managed clusterでは`TartControlPlane.spec.version`の変更がtriggerとなる。開始前にworkerの`Machine.spec.version`またはMachineDeployment desired versionが目標versionと矛盾しないことを確認し、矛盾時は`Ready=False`、`Reason=VersionSkew`としてControl Plane Providerはworker resourceを変更しない。

full machine configurationの再applyでは、current CAPI versionを必ず反映する。live configurationのKubernetes component image versionを古いdigestで上書きしないため、version-managed fieldをuser patchから分離する。

通常更新では同じCAPI Machine、`TartMachine`、`TartHost`、diskを維持する。初回provisioning後のmutableなTalos OS/config updateを実行するのはUpdate Extensionだけであり、通常のInfrastructure/Bootstrap reconcileはdesired/actual diffを観測してStatusを更新する。`CanUpdateMachineSet`/`CanUpdateMachine`はold/new双方の`configSecretRef`を解決してrenderしたeffective configurationのsemantic diff全体を判定し、Secret参照名だけで安全性を推測しない。missing、unreadable、generation不明はunknownとして、unsafe、unknown、partial diffはpatchなしの`Failure`でvetoする。各CAPI minorでMachineSet、Machine、TartHost claimが作成されないことをE2Eで確認し、確認できないminorを対応範囲へ含めない。node-disruptive operationの前にはTalosの安全なdrainまたはworkload cluster側のcordon/drainを試す。drain失敗がavailability、PDB、capacityだけの理由で`TartCluster.spec.updatePolicy.allowDowntime: true`が明示されていればgraceful shutdown/rebootを許容し、未指定または`false`ならavailability理由でも開始しない。destructive disk change、identity mismatch、Host mismatch、unsafe etcd membership change、quorum violationは緩和しない。Talos rollback後はdesired Specを自動修正せず、`UpdateMachine`を`Failure`、`Reason=RolledBack`として後続のControl Plane updateを停止する。CAPIのrolloutがimmutable差分からreplacementを提案する可能性がある場合も、Tartは安全に更新できない差分を`Ready=False`と安全なreasonで報告し、Hostの自動再利用を許可しない。Tart v1alpha1では自動replacementやguided reprovisionのopt-inを提供しない。

## Rolling updateとMachineHealthCheck

TartのWorker標準rollout profileは、対応するCAPI resourceの設定で`maxSurge: 0`、`maxUnavailable: 1`を推奨する。`maxUnavailable: 0`ではCAPIがbufferのためsurge Machineを作成し得るため、追加Hostを必要とする設定として明示する。Control Planeのin-place updateはTartControlPlaneが`CanUpdateMachine`成功後にMachine、InfraMachine、BootstrapConfigをannotation付きで更新し、Machineへ`UpdateMachine` hook pendingを設定して一台ずつ進める。Tartは独自のrollout controllerや`maxUnavailable`実装を作らない。

Tartはlocal persistent stateの有無を判定しないため、すべてのTart-managed MachineでMHCのdelete-and-recreate remediationを安全な既定値とみなさない。初期運用ではMachineSetまたはControl PlaneのMachine templateへMachine生成前から`cluster.x-k8s.io/skip-remediation`を設定し、Machine作成後の後追いannotationだけに依存しない。Tart v1alpha1では自動replacementやguided reprovisionのopt-inを提供しない。利用者のMachine削除はCAPIの通常replacement semanticsを発生させ得て、別のAvailable Hostをclaimする可能性がある。Retained Hostの`Reprovision`承認はdata破棄だけを許可し、Machine削除や同じHostへの再割り当てを開始しない。将来のexternal remediationは、同じMachine、TartMachine、TartHostを維持したままpower cycle、Talos recovery、health確認を行う。

## Deletion

```text
CAPI Machine controllerがdrain / volume detachを完了
        ↓
Control Plane Providerがscale-down用pre-terminate delete hookでetcd member removalを完了
        ↓
TartMachine finalizerがauthenticated Talos APIへshutdown/quiesceを要求
        ↓
Hostの停止確認
        ↓
TartHost.spec.retainedFromへ直前のconsumerを記録
        ↓
TartHost.spec.consumerRefを解除
        ↓
物理dataを保持したRetained Hostとして残す
```

Talos APIに到達できない、shutdownの結果を確認できない、またはHostが停止していない場合はclaimとfinalizerを解放せず、`Ready=False`、`Reason=ShutdownUnconfirmed`とする。BMC/VM backendではout-of-bandの`PowerOff`を確認し、WoL-onlyまたはmanual backendではauthenticated Talos `Shutdown` RPCの受理後にendpointが一定時間消失したことを観測する。後者は物理電源OFFの証明ではなく、旧clusterへ接続可能なTalosが動作し続けていないことの確認である。停止確認前にclaimを解放してはならない。

Machine deletionはdata retentionを意味し、disk wipe、cleaning、reprovisioningを意味しない。Cluster全体のdeletionではetcd quorum維持を削除完了の前提にしない。Cluster secret bundleはCluster存続中に過去generationをGCせず、deletion時はManaged Machineのshutdownとretention、DR保持方針を確認した後だけGCする。bundle消失後のRetained Hostは`Adopt`不可、`Reprovision`専用である。`TartHost`の直接削除はClaim中またはRetainedなら明示的なforget annotationが必要で、forget承認後もpower off、reset、disk wipeを行わない。明示的なforce releaseやdestructive cleanを将来追加する場合も、通常updateや通常deleteから暗黙に呼び出さず、別の権限、確認、監査、Conditionを要求する。

## Conditionsとerror

Condition typeはResourceごとに固定する。`TartHost`は`Ready`、`InventoryReady`、`TalosReachable`、`Claimed`、`Retained`、`Reusable`、`TartCluster`は`Ready`、`TartMachine`は`Ready`、`TalosReachable`、`Provisioned`、`UpToDate`、`TartBootstrapConfig`は`Ready`、`TartControlPlane`は`Ready`、`Available`、`UpToDate`、`RollingOut`、`ScalingUp`、`ScalingDown`、`MachinesReady`、`MachinesUpToDate`、`EtcdClusterAvailable`、`Deleting`、`Paused`を使う。CAPI-facing Resourceの安全停止は汎用`Blocked` Condition typeではなく`Ready=False`または`Available=False`と具体的なreasonで表す。`PreparingBoot`、`Writing`、`Verifying`のようなworkflowのprogram counterは保存しない。

電源投入待ち、DHCP address待ち、maintenance API待ち、reboot中、Kubernetes APIの一時的なunavailableは、再試行可能なConditionとrequeueとして扱う。identity mismatch、無効なdisk selector、destructive change、quorumを守れないscale down、対応していないupdate pathは、retryを続けず`Ready=False`と具体的なreasonへ遷移させる。

外部API call直後にcontrollerが停止しても、再起動後はversion、reachability、health、configuration digest、ProviderID、etcd membership、Secret、Node状態を観測して未開始・実行中・完了済み・失敗を判断する。API callを送った事実だけをStatusへ保存しない。
