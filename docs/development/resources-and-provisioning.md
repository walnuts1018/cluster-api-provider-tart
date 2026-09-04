# リソースとProvisioningの流れ

この文書は、TartのResource Modelを、Cluster APIのreference、所有関係、観測結果、外部副作用へ対応付ける。処理の進捗を保存するOperation Resourceは存在しない。

## Resourceの責務

| Resource | 寿命 | Specの正本 | Statusに保存する観測 |
|---|---|---|---|
| `TartHost` | 物理/仮想Hostの寿命 | Host identity、power/boot capability、選択用label | inventory、addresses、reachability、claim、Conditions |
| `TartCluster` | CAPI `Cluster`の寿命 | control plane endpointとcluster-level infrastructure | endpoint反映、provisioned、failure domains、Conditions |
| `TartMachine` | CAPI `Machine`の寿命 | Host selection、desired Talos image、machine infrastructure identity | Host binding、Talos version、addresses、ProviderID反映、provisioned、Conditions |
| `TartMachineTemplate` | templateの寿命 | `TartMachine`のtemplate Spec | Statusなし、または標準的なConditionsのみ |
| `TartBootstrapConfig` | CAPI Machineのbootstrap dataの寿命 | machine role、Kubernetes version、Talos-native config patches、cluster secret reference | Secret生成、configuration digest、Conditions |
| `TartBootstrapConfigTemplate` | templateの寿命 | `TartBootstrapConfig`のtemplate Spec | Statusなし、または標準的なConditionsのみ |
| `TartControlPlane` | CAPI Clusterのcontrol planeの寿命 | version、replicas、machine template | replica counts、version、control plane initialized、selector、Conditions |
| `TartControlPlaneTemplate` | templateの寿命 | `TartControlPlane`のtemplate Spec | Statusなし、または標準的なConditionsのみ |

`TartMachine`はCAPI `Machine`と1対1で対応し、通常はCAPI `Machine`がownerとなる。`TartHost`はMachineより長寿命なのでMachineのOwnerReferenceを設定しない。Machine削除時にはHostのclaimだけを解除し、Host resourceと物理disk上のdataを保持する。

`TartBootstrapConfig`が作成するSecretはbootstrap dataの配布物であり、Statusへ内容を複製しない。Secret名はCAPI Bootstrap contractの`status.dataSecretName`から参照できるようにする。SecretのdataにはTalos configurationと、必要なcluster secret materialを格納するが、log、Event、Condition messageへ値を出力しない。

## 正本とcache

| 情報 | 正本 | Statusに置けるもの |
|---|---|---|
| Cluster topology、replica、Kubernetes desired version | Cluster API | 現在の観測値 |
| Host inventory、allocation | `TartHost` | controllerが取得した最新inventory |
| Machineのdesired infrastructure | `TartMachine` | Talos version、ProviderID、addressesの観測 |
| Talos desired configuration | `TartBootstrapConfig`とCAPI desired state | configuration digest、生成済みSecret名 |
| Talos actual configuration、version、disk | Talos API | observed version、reachability、inventory |
| Kubernetes actual health | workload Kubernetes APIとTalos API | ready/available counts、Conditions |

同じdesired stateを複数Resourceへコピーして正本にしない。Statusのdigestやversionはcacheであり、次回reconcileでは外部APIとdesired stateを再確認する。

## Host claim

Host選択は、明示的な`hostRef`があればそれを優先し、なければarchitecture、labels、hardware capability、availabilityを満たすHostからdeterministicに選択する。選択結果は`TartHost.status.claimedBy`と`TartMachine.status.hostRef`に観測として反映する。

claimは同じHostを複数Machineが利用しないよう、取得直後のresourceVersionとUIDを検証してserver-side applyする。競合した場合は別Hostを再選択する。既存claimが別Machineを指している場合に強制的に上書きしてはならない。

claim解除、Hostの再利用、物理dataの破棄は別の意味を持つ。通常のMachine削除ではclaimだけを解除し、Hostを再利用可能と表示する場合も、前回のTalos installationやdisk dataが残っていることをStatusとドキュメントで明示する。

## Fresh Machine

```text
CAPI Machine / TartMachine作成
        ↓
Hostをdeterministicに選択してclaim
        ↓
power onまたはboot backendへmaintenance bootを要求
        ↓
maintenance Talos APIからidentity、address、hardware inventoryを取得
        ↓
TartHost.statusへinventoryを反映
        ↓
Bootstrap Providerが生成したSecretからmachine configurationを取得
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

Tartはblock deviceへimageを書き込まず、partition tableを直接編集しない。installer disk、volume、encryption、system extension、kernel module、extra manifestなどはBootstrap ConfigのTalos-native configurationとして指定する。

maintenance modeからauthenticated APIへの切り替えは、固定のstep番号ではなく、到達可能なendpoint、identity、Talos version、configurationの観測結果で判断する。controller再起動後も同じ観測から継続できる。

## Control Planeの初回bootstrapとscale

Control Plane Providerは最初のcontrol plane Machineを選び、Talos APIのbootstrapを一度だけ要求する。要求後にcontrollerが停止しても、次回はTalosのetcd/Kubernetes healthとCluster APIの初期化状態を先に確認し、bootstrap済みのclusterへ再初期化を送らない。

HA構成のscale upでは、既存clusterがhealthyであることを確認してから新しいMachineを作成し、Talos configurationを適用する。scale downでは、対象memberがetcd quorumを壊さず、Talosがmember removalを安全に完了できることを観測してからMachine削除へ進む。安全性を判定できない場合は削除せず、`Blocked`または`UnsafeControlPlaneOperation`をConditionへ設定する。

## Update

更新は次の4種類を別々に扱う。

| 変更 | 原則 | 完了の観測 |
|---|---|---|
| Talos OS version/image | 既存Machine上でTalos upgrade APIを呼ぶ | desired version、reboot後のreachability、health |
| Talos machine configuration | Talos APIが許可する場合だけapplyする | Talosのactual configurationとdigest、health |
| Kubernetes version | CAPI desired versionを正本にControl Plane Providerがsequenceする | control plane/workerのversionとCAPI Conditions |
| Host identity、破壊的disk topology | 自動更新しない | `UnsafeChange`または`RequiresExplicitReprovision` |

通常更新では同じCAPI Machine、`TartMachine`、`TartHost`、diskを維持する。CAPIのrolloutがimmutable差分からreplacementを提案する可能性がある場合も、Tartが保護対象Machineについて安全なin-place更新不能を明示し、破壊的fallbackを暗黙に開始しない。

複数Machineのrolling update順序と停止数はCAPIのrollout policyに従う。Tartは独自の`maxUnavailable`やrollout controllerを複製しない。single nodeではdowntimeを許容するが、同一Machineを維持したままupgradeとrebootを行う。

## Deletion

```text
CAPI Machine削除
        ↓
TartMachineのfinalization
        ↓
Host claim解除
        ↓
物理dataは保持
        ↓
Hostは明示的な再利用またはreprovisioningを待つ
```

削除はupdateと異なり、ユーザーが明示的に要求したlifecycleである。それでも`TartMachine`削除をdisk wipeの合図にはしない。cleaningやreprovisioningを将来追加する場合は、通常updateから呼び出せない明示的な操作、権限、Condition、監査記録を設ける。

## Conditionsとエラー

Statusには`Ready`、`Claimed`、`TalosReachable`、`Provisioned`、`UpToDate`、`Updating`、`Healthy`、`Blocked`など外部から意味を理解できるConditionだけを置く。`PreparingBoot`、`Writing`、`Verifying`のようなworkflowのprogram counterは保存しない。

電源投入待ち、DHCP address待ち、maintenance API待ち、reboot中、Kubernetes APIの一時的なunavailableは、再試行可能なConditionとrequeueとして扱う。identity mismatch、無効なdisk selector、destructive change、quorumを守れないscale down、対応していないupdate pathは、retryを続けず明確なblocked Conditionへ遷移させる。

## Secretとnetwork boot

初期boot assetはsecret-freeを基本とする。bootstrap dataはfirmwareのboot protocolや公開HTTPへ埋め込まず、maintenance Talos APIへ認証済みconfigurationを送る経路でdeliveryする。外部network boot、Wake-on-LAN、Redfish、VM API、手動起動は`boot`のbackendとして差し替え可能にするが、Tart独自のAgent protocolや長寿命credentialをHostへ要求しない。

秘密情報を扱う処理では、Secretの値、Talos client key、Kubernetes PKI private key、kubeconfig、BMC passwordをStatus、Event、通常log、metrics labelへ含めない。metricsやEventにはresource name、reason、safeなerror分類だけを出す。
