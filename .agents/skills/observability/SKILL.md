---
name: observability
description: Tartのlog、Condition、Event、metrics、retryとOpenTelemetry境界を確認する
when_to_use: observability、外部APIのretry、error分類、log、Event、metricsを実装・レビューする時
---

# Observabilityとretry

## メッセージ

log、Event、Condition message、Status messageは英語で書き、ユーザーが次に取るべき行動や再試行可能性が分かるreasonを含める。秘密情報、Secret値、credential、private key、Bootstrap Data、kubeconfigを出さない。metrics labelにもresource nameと安全な分類以外を入れない。

## Conditions

Conditionは外部から観測できる能力や状態を表す固定typeとし、汎用の`Blocked` Condition typeは追加しない。安全停止は`Ready=False`または`Available=False`とreason（例:`UnsafeUpdate`、`IdentityConflict`、`RolledBack`、`SecretBundleUnavailable`）で表し、controllerのstep番号、retry回数、goroutineの状態を表現しない。`observedGeneration`はdesired Specを観測したgenerationへ更新する。
Statusへ公開するconfiguration digestはsecret-bearing valueをredaction markerへ置換したcanonical semantic representationのSHA-256とし、secret値を含む内部比較結果やHMACをStatus、Event、log、metricsへ出力しない。更新安全性は公開digestではなく、解決済みSecretからrenderしたsemantic diffで判定する。

## error分類

power待ち、DHCP/address待ち、maintenance API待ち、reboot、Kubernetes APIの一時的unavailableはtransient errorとしてbounded requeueする。identity mismatch、destructive change、quorum violation、unsupported update path、desired versionからのrollbackはpermanentまたは安全停止として`Ready=False`と具体的なreasonへ反映する。rollback後にdesired Specをactual versionへ自動修正しない。

## retry

ネットワーク越しの処理にretryを追加する場合は、context deadline、最大試行回数、指数backoff、最終的なConditionを定義する。副作用を送った直後に成功とみなさず、次のreconcileで外部状態を観測する。同じEventをretryごとに大量発行しない。

## OpenTelemetry

TracerとMeterはグローバルな`otel.GetTracerProvider()`と`otel.GetMeterProvider()`から取得する。sampling rate、export先、endpointなどをTart独自の固定値や独自環境変数parserで上書きしない。spanとmetricへcredentialや高cardinalityのhardware detailを入れない。
