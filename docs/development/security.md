# セキュリティと観測性

## Trust boundary

一般的なPCではTPM attestationやBMC identityを利用できない場合がある。初回provisioning networkはtrusted infrastructureとして明示的なsecurity boundaryに含め、MAC、DHCP情報、system UUID、Talos hardware inventoryなど利用可能なidentityを組み合わせる。完全なhardware identity証明を初回から必須条件にしないが、期待したHostとmaintenance endpointの論理的なbindingは必須とする。

未構成Talosのmaintenance APIはTLSで暗号化されるが認証済みではない。self-signed certificate、client certificateなし、相互のidentity検証なしというtrust modelである。machine configurationを送る前に、`TartHost`、決定的に再構成できるboot authorization/correlation identity、MAC/DHCP、endpoint、observed system UUID/inventory、利用可能ならcertificate fingerprintを結び付ける。一つでも曖昧、競合、identity mismatchがあればconfigurationをapplyせず`Ready=False`、`Reason=IdentityConflict`にする。MAC、system UUID、inventoryは誤接続防止の照合材料であってactive attackerに対する暗号学的認証ではなく、provisioning networkをactive attackerから信頼することがsecurity assumptionである。

installation後はauthenticated Talos APIの相互TLSへ移行する。maintenance modeの暗号化をauthenticated APIの認証として扱わない。firmware boot protocolや公開HTTPへcluster credentialを埋め込まず、初期boot assetはsecret-freeとする。

## Secret lifecycle

次の情報をCR Status、Condition message、Event、通常log、metrics label、debug artifactへ出力しない。

- Talos machine secrets
- Kubernetes PKI private key
- Talos client key
- Bootstrap Data
- kubeconfig、token、credential
- BMC password
- private signing material

Talos/Kubernetesのcluster-level PKIとsecret materialは`TartCluster.spec.id`を含むgeneration単位で生成する。Control Plane Providerがcontrol-plane MachineとBootstrap Secretより先にactive generationを作成し、Bootstrap Providerはread-onlyで参照する。BootstrapConfigごとにgenerateせず、Cluster namespaceの決定論的な`<cluster-name>-talos-secrets-<cluster-id>-g<generation>` Secretを正本として全Machineで共有する。各Secretはimmutableとし、CA rotationはTalosの段階的なCA rotation operationへ委譲する。rotation開始前にTalosの準備結果から得た新しいsecret materialで次generationのimmutable Secretを`Pending`として永続化し、accepted CA追加、issuing CA切替、certificate refresh、旧CA削除の正常完了を観測してからactive generationを永続参照から切り替える。rotation中は新しいMachineのprovisioningとAdoptを開始しない。Cluster存続中は過去generationをGCせず、Cluster削除時に全Managed Machineのshutdownとretention、バックアップ保持方針、Retained Hostの再利用制約を確認した後だけGCを許可する。必要なgenerationが欠落またはactive generationが不明な場合は自動再生成せず、`Ready=False`、`Reason=SecretBundleUnavailable`として報告する。Cluster削除後に残るRetained Hostはこのcredentialを使った`Adopt`を許可せず、`Reprovision`だけを許可する。

Bootstrap SecretはCAPI contractに合わせ、type `cluster.x-k8s.io/secret`、単一の`value` key、決定論的なSecret名、cluster label、対応するBootstrapConfigのcontroller OwnerReferenceを持つ。`value`には対象Machine向けのcomplete Talos machine configurationを格納し、cluster bundleを独自keyへ分解しない。

ユーザーのraw configuration patchは全てimmutableな`configSecretRef`のSecret-backed inputへ格納する。Secretには非機密configurationを含めてもよいが、CRD Specへ任意のraw patchをinlineで保存する経路は提供しない。これによりTalosの将来schemaに追加されるsecret-bearing fieldを不完全なdenylistで見落とすことを避ける。参照先Secretのin-place変更でdesired diffを隠さず、内容変更は新しいSecret名とreference変更で表す。

Control Plane Providerは`<cluster-name>-kubeconfig` Secretを生成・維持する。Bootstrap Secretと同じtype、label、single `value` keyを使い、TartControlPlaneのcontroller OwnerReferenceを設定する。client certificateを使用する場合は短い有効期間と更新を設計し、期限切れをobserved stateから検出する。

Secretの値をStatusへ複製せず、StatusにはSecret名、生成済みかどうか、secret-bearing valueをredactしたcanonical semantic representationのSHA-256 digestだけを置く。Secret名もcredentialそのものではないことを確認し、clusterやMachineのidentity以上の情報を含めない。更新安全性は公開digestではなく、old/new Secretを解決したsemantic diffで判定し、secret値を含む内部比較結果を永続化・出力しない。

## Host retentionとdeletion safety

Machine削除時はCAPI Machine controllerのdrain/volume detach、scale-down時のControl Plane Providerによるpre-terminate delete hookのetcd member removal、安全なTalos shutdown、停止確認の順で処理し、停止を確認するまで`TartHost.spec.consumerRef`を解除しない。確認不能ならfinalizerとclaimを保持し、`Ready=False`、`Reason=ShutdownUnconfirmed`へ反映する。claim解除時は`spec.retainedFrom`へ直前のconsumerを記録する。Hostはdataを保持した`Retained`であり、現在のretained UIDに一致する明示的な`Adopt`または`Reprovision`承認がそろうまで自動allocation対象に戻さない。Cluster全体のdeletionではetcd quorum維持を必須にしない。

MHCやCAPI rolloutによるdelete-and-recreateも同じ保護境界に含める。Tartはlocal persistent stateの有無を判定しないため、初期運用ではMachineSetまたはControl PlaneのMachine templateへMachine生成前から`cluster.x-k8s.io/skip-remediation`を設定し、Machine作成後の後追いannotationだけに依存せず、MHCのreplacement remediationを既定で許可しない。replacementの明示的opt-inなしにMachineを削除しない。force release、clean、reprovisioningは通常updateやdeleteの暗黙の副作用にしない。`TartHost`の直接削除も同様に、Claim中またはRetainedのHostは`tart.cluster.x-k8s.io/forget-approved: "true"` annotationなしに削除せず、forget承認後もpower off、reset、disk wipeを行わない。
Tart v1alpha1では自動replacementのopt-inを提供しない。再構築が必要な場合は、利用者がMachineを明示的に削除し、Retained Hostへ現在の`retainedFrom.uid`に一致する`Reprovision`承認を設定する。single-nodeのnode-disruptive updateでavailabilityよりdata preservationを優先する正本は`TartCluster.spec.updatePolicy.allowDowntime: true`とし、未指定または`false`なら開始しない。

Host identityの一意性はadmission webhookの全体list検査だけで保証しない。同時createで検査がraceするため、controllerが`spec.id`、MAC、system UUID、disk identityの重複を観測したら関係する全Hostを`Ready=False`、`Reason=IdentityConflict`としてallocationとmaintenance configuration applyから除外する。

management clusterのDRでは、`TartHost.spec.id`、`TartCluster.spec.id`、stable hardware identity、consumerRef、retainedFrom、CAPI Machine/provider resource、全secret bundle generation、power/boot設定を同じ整合点からバックアップする。復元後はClusterやHostのobject metadata UID変更を永続identityの変更と解釈せず、関係するResource、Cluster ID、active bundle generationを観測してから副作用を再開する。同名Clusterの再作成では新しいCluster IDを要求し、古いbundleやRetained Hostを再関連付けしない。`clusterctl move`でこの復元契約を代用せず、bundleが欠落または世代不明なら`Adopt`せず`Reprovision`または明示的な管理者復旧を要求する。

## Least privilege

Infrastructure、Bootstrap、Control Plane ProviderのKubernetes権限を責務ごとに分ける。network boot backendとcluster administrative credentialを同じ権限へまとめる必要はない。別API groupのprovider resourceをCAPI coreが扱うために必要なaggregated RBACだけを追加し、それ以外の権限を与えない。

controllerは必要なnamespace、Resource、Status、Secretへの最小権限だけを持つ。cluster secret bundleを扱うBootstrap/Control Planeの処理と、secret-free boot assetを配布するboot backendのtrust boundaryを必要に応じて分離する。

`TartHost.spec.consumerRef`、`spec.reuseApproval`、`spec.reuseMode`、`spec.reusePolicy`へwriteできる主体は、Host上のTalos installationやpersistent dataを破壊し得るinfra administratorとして扱う。Kubernetes RBACにはfield-level permissionがないため、これらのSpecをwriteできるRoleとcontrollerの権限を分離し、通常のworkload operatorへ付与しない。controllerはTartHostの全writeを安全なvalidation、Retained gate、identity再確認、監査可能なEventへ通す。

## Runtime Extensionの前提

in-place updateを使用するmanagement clusterでは、CAPIの`RuntimeSDK=true`と`InPlaceUpdates=true` feature gateを有効にする。TartのHTTPS endpointを`ExtensionConfig`へ登録し、server certificate、TLS Secret、必要なCA trustを明示的に管理する。現行CAPIではin-place update hookへ登録できるextensionは1つに制限されるため、Tart以外のhookを同時に登録しない。

Runtime Extensionが未登録またはTLS検証に失敗した場合はin-place updateを有効にしない。`CanUpdateMachineSet`/`CanUpdateMachine`が対象差分を全てcoverできない場合はpatchなしの`Failure`を返し、Successの部分patchでCAPIにimmutable rolloutを選ばせない。TartMachineの`Ready=False`判定、Hostの`Retained` gate、rollout profile、MHC policyは追加の安全境界として併用する。

## Log、Event、metrics

logはreconcileの対象、原因、結果、safeなerror分類をstructured fieldで追跡できるようにする。Eventはユーザーの行動が必要な重要なlifecycle eventだけへ限定し、retryごとに重複発行しない。resource name、reason、provider roleなど低cardinalityのfieldを使い、hardware inventoryやcredentialをmetrics labelへ入れない。

Resourceごとに[API contract](api-contract.md)で固定したConditionだけを使う。CAPI-facing Resourceの安全停止は汎用`Blocked` Condition typeではなく`Ready=False`または`Available=False`と具体的なreasonで表す。`PreparingBoot`、`Writing`、`Verifying`、`BootTrial`などcontroller内部のstepをCondition typeとして乱立させない。

## Retry

power待ち、address待ち、maintenance API待ち、reboot、Kubernetes APIの一時的unavailableはtransient errorとしてrequeueする。backoffを定義し、無制限のbusy loopを作らない。identity mismatch、destructive change、unsupported operation、quorum violation、停止未確認のdeletionは安全停止として`Ready=False`とreasonへ反映する。

## OpenTelemetry

TracerとMeterはグローバルな`otel.GetTracerProvider()`と`otel.GetMeterProvider()`から取得する。sampling rate、export先、endpointをTart独自の固定値や独自環境変数parserで上書きしない。span、metric、eventへSecret、private key、高cardinalityのdisk詳細を含めない。
