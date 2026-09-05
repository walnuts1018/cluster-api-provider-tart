# セキュリティと観測性

## Trust boundary

一般的なPCではTPM attestationやBMC identityを利用できない場合がある。初回provisioning networkはtrusted infrastructureとして明示的なsecurity boundaryに含め、MAC、DHCP情報、system UUID、Talos hardware inventoryなど利用可能なidentityを組み合わせる。完全なhardware identity証明を初回から必須条件にしないが、期待したHostとmaintenance endpointの論理的なbindingは必須とする。

未構成Talosのmaintenance APIはTLSで暗号化されるが認証済みではない。self-signed certificate、client certificateなし、相互のidentity検証なしというtrust modelである。machine configurationを送る前に、`TartHost`、boot attempt、MAC/DHCP、endpoint、observed system UUID/inventory、利用可能ならcertificate fingerprintを結び付ける。一つでも曖昧、競合、identity mismatchがあればconfigurationをapplyせず`Blocked`にする。

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

Talos/Kubernetesのcluster-level PKIとsecret materialはClusterごとに一度だけ生成する。BootstrapConfigごとにgenerateせず、Cluster namespaceの決定論的な`<cluster-name>-talos-secrets` Secretを正本として全Machineで共有する。Secretは初期化前に作成し、immutableとして扱う。初期化後に欠落した場合は自動再生成せず、cluster identityを守るため`Blocked`とする。

Bootstrap SecretはCAPI contractに合わせ、type `cluster.x-k8s.io/secret`、単一の`value` key、決定論的なSecret名、cluster label、対応するBootstrapConfigのcontroller OwnerReferenceを持つ。`value`には対象Machine向けのcomplete Talos machine configurationを格納し、cluster bundleを独自keyへ分解しない。

Control Plane Providerは`<cluster-name>-kubeconfig` Secretを生成・維持する。Bootstrap Secretと同じtype、label、single `value` keyを使い、TartControlPlaneのcontroller OwnerReferenceを設定する。client certificateを使用する場合は短い有効期間と更新を設計し、期限切れをobserved stateから検出する。

Secretの値をStatusへ複製せず、StatusにはSecret名、生成済みかどうか、非可逆なconfiguration digestだけを置く。Secret名もcredentialそのものではないことを確認し、clusterやMachineのidentity以上の情報を含めない。

## Host retentionとdeletion safety

Machine削除時はCAPIのdrain、control planeのetcd detach、安全なTalos shutdown、停止確認の順で処理し、停止を確認するまで`TartHost.spec.consumerRef`を解除しない。確認不能ならfinalizerとclaimを保持し、`Blocked`へ反映する。claim解除後のHostはdataを保持した`Retained`であり、明示的に`Reusable`へ変更されるまで自動allocation対象に戻さない。

MHCやCAPI rolloutによるdelete-and-recreateも同じ保護境界に含める。local persistent stateを持つMachineでは、初期運用として`cluster.x-k8s.io/skip-remediation`を使用し、MHCのreplacement remediationを既定で許可しない。force release、clean、reprovisioningは通常updateやdeleteの暗黙の副作用にしない。

## Least privilege

Infrastructure、Bootstrap、Control Plane ProviderのKubernetes権限を責務ごとに分ける。network boot backendとcluster administrative credentialを同じ権限へまとめる必要はない。別API groupのprovider resourceをCAPI coreが扱うために必要なaggregated RBACだけを追加し、それ以外の権限を与えない。

controllerは必要なnamespace、Resource、Status、Secretへの最小権限だけを持つ。cluster secret bundleを扱うBootstrap/Control Planeの処理と、secret-free boot assetを配布するboot backendのtrust boundaryを必要に応じて分離する。

## Runtime Extensionの前提

in-place updateを使用するmanagement clusterでは、CAPIの`RuntimeSDK=true`と`InPlaceUpdates=true` feature gateを有効にする。TartのHTTPS endpointを`ExtensionConfig`へ登録し、server certificate、TLS Secret、必要なCA trustを明示的に管理する。現行CAPIではin-place update hookへ登録できるextensionは1つに制限されるため、Tart以外のhookを同時に登録しない。

Runtime Extensionが未登録、TLS検証に失敗、またはhookが対象差分を扱えない場合、CAPIがimmutable rolloutへfallbackし得る。したがってRuntime Extensionを安全性の唯一の防波堤にせず、TartMachineのblocked判定、Hostの`Retained` gate、rollout profile、MHC policyを併用する。

## Log、Event、metrics

logはreconcileの対象、原因、結果、safeなerror分類をstructured fieldで追跡できるようにする。Eventはユーザーの行動が必要な重要なlifecycle eventだけへ限定し、retryごとに重複発行しない。resource name、reason、provider roleなど低cardinalityのfieldを使い、hardware inventoryやcredentialをmetrics labelへ入れない。

`Ready`、`TalosReachable`、`Provisioned`、`UpToDate`、`Updating`、`Healthy`、`Blocked`、`Retained`、`Reusable`などのConditionは外部の能力と状態を表す。`PreparingBoot`、`Writing`、`Verifying`、`BootTrial`などcontroller内部のstepをCondition typeとして乱立させない。

## Retry

power待ち、address待ち、maintenance API待ち、reboot、Kubernetes APIの一時的unavailableはtransient errorとしてrequeueする。backoffを定義し、無制限のbusy loopを作らない。identity mismatch、destructive change、unsupported operation、quorum violation、停止未確認のdeletionは安全停止としてblockedへ反映する。

## OpenTelemetry

TracerとMeterはグローバルな`otel.GetTracerProvider()`と`otel.GetMeterProvider()`から取得する。sampling rate、export先、endpointをTart独自の固定値や独自環境変数parserで上書きしない。span、metric、eventへSecret、private key、高cardinalityのdisk詳細を含めない。
