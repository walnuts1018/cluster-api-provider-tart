# セキュリティと観測性

## Trust boundary

一般的なPCではTPM attestationやBMC identityを利用できない場合がある。初回provisioningはtrusted infrastructureとして明示したnetwork boundaryで行い、MAC、system UUID、Talos hardware inventoryなど利用可能なidentityを組み合わせる。完全なhardware identity証明を初回から必須条件にしない。

maintenance Talos APIは初回のhardware discoveryとconfiguration deliveryに限定して利用し、installation後はauthenticated Talos APIへ切り替える。maintenance modeのtrust modelをauthenticated APIと同一視しない。

## Secret

次の情報をCR Status、Condition message、Event、通常log、metrics label、debug artifactへ出力しない。

- Talos machine secrets
- Kubernetes PKI private key
- Talos client key
- Bootstrap Data
- kubeconfig、token、credential
- BMC password
- private signing material

Bootstrap dataはKubernetes Secretへ格納し、firmwareのboot protocolや公開HTTPへ埋め込まない。初期boot assetはsecret-freeを基本とする。Secretの値をStatusへ複製せず、StatusにはSecret名、生成済みかどうか、非可逆なconfiguration digestだけを置く。

## Least privilege

Infrastructure、Bootstrap、Control Plane ProviderのKubernetes権限を責務ごとに分ける。network boot backendとcluster administrative credentialを同じ権限へまとめる必要はない。controllerは必要なnamespace、Resource、Status、Secretへの最小権限だけを持つ。

Hostを別Machineへ再利用する処理は、既存dataを保持したままではinstallation済みidentityとdesired identityが混在し得る。再利用またはreprovisioningを実装する場合は、通常updateとは別の権限、明示的な確認、監査可能なConditionを要求する。

## Log、Event、metrics

logはreconcileの対象、原因、結果、safeなerror分類をstructured fieldで追跡できるようにする。Eventはユーザーの行動が必要な重要なlifecycle eventだけへ限定し、retryごとに重複発行しない。resource name、reason、provider roleなど低cardinalityのfieldを使い、hardware inventoryやcredentialをmetrics labelへ入れない。

`Ready`、`TalosReachable`、`Provisioned`、`UpToDate`、`Updating`、`Healthy`、`Blocked`などのConditionは外部の能力と状態を表す。`PreparingBoot`、`Writing`、`Verifying`、`BootTrial`などcontroller内部のstepをCondition typeとして乱立させない。

## Retry

power待ち、address待ち、maintenance API待ち、reboot、Kubernetes APIの一時的unavailableはtransient errorとしてbounded requeueする。timeout、最大試行回数、backoffを定義し、無制限のbusy loopを作らない。identity mismatch、destructive change、unsupported operation、quorum violationは安全停止としてblockedへ反映する。

## OpenTelemetry

TracerとMeterはグローバルな`otel.GetTracerProvider()`と`otel.GetMeterProvider()`から取得する。sampling rate、export先、endpointをTart独自の固定値や独自環境変数parserで上書きしない。span、metric、eventへSecret、private key、高cardinalityのdisk詳細を含めない。
