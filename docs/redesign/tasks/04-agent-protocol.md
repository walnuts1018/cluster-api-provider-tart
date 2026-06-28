# Task 04: Agent protocolと安全な配信

## 目的

一時Provisioning Agentがoperationを安全に取得し、再接続・重複送信可能なversioned protocolを実装する。

## 依存

- Task 02のoperation model
- Task 05のartifact manifest契約

## 実装範囲

- `/v1`でversion管理するagent registration、plan、progress、bootstrap bundle、boot confirmation API
- host、operation、期限へbindingしたsession token
- token hashの永続化、単回消費、rate limit
- TLS、request size limit、deadline、replay防止
- operation phaseとagent sequence numberによる重複・順序逆転対策
- CAPI Bootstrap Secretから1レスポンスのbundleを生成するadapter
- Bootstrap Secretの`value`、`format`、digestを保持するbundle schema
- platform別initial credential bootstrapと脅威モデル
- 監査EventとOpenTelemetry span

TokenをURL queryへ入れずAuthorization header等で送る。最初のcredentialを取得する前提として、TPM/事前登録host key/BMC保護mediaまたは隔離provisioning L2のいずれを使うかをprofileへ明記する。agentの`disk write completed`だけではMachineをReadyにしない。

## 受け入れ条件

1. tokenを別host、別operation、期限後に利用できない。
2. Bootstrap bundleは正常な1レスポンス後に再取得できない。
3. 切断後の同じprogress reportを安全に再送できる。
4. 古いsequence、未知phase、過大body、不正content typeを拒否する。
5. controller再起動後もtokenとoperationを復元できる。
6. log、Event、Status、trace attributeにtokenまたはBootstrap Dataがない。
7. race detectorを含む並行token消費テストで1 requestだけ成功する。
8. initial credentialが公開iPXE script、kernel command lineの監査log、HTTP access logへ出ない。
9. hardware identityを持たないprofileを悪意あるL2参加者に安全と表示しない。
10. unsupportedなBootstrap `format`と未分離pathへ書くcustomizationを処理前に拒否する。

## 対象外

- disk処理本体
- distribution固有bootstrap scriptの生成
- 汎用的なremote shell API

## 関連

- ADR 0004、0006
- Issue #147
