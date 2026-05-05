# Multi OS Bootstrap Metadata Design

## 背景

`cluster-api-provider-tart` は OS / Bootstrap 非依存の Pull 型プロビジョニングを目指しているが、現在の HTTP サーバーは Talos に寄った形で実装されている。`/ipxe` は bootstrap token が存在すると常に `talos.config=<metadata URL>` を kernel parameter に追加し、`/metadata/:namespace/:name` は CAPI `Machine.spec.bootstrap.dataSecretName` が指す Secret の `data.value` を単一ファイルとして返す。

この挙動は Talos では自然だが、Ubuntu autoinstall、Debian cloud-init、Debian Installer preseed、kubeadm、k3s を同じ仕組みで扱うには metadata の配信形式が不足している。特に cloud-init NoCloud は base URL 配下の `user-data` と `meta-data` を要求し、Debian Installer は `preseed.cfg` を kernel parameter の `url=` で取得する。

## 目的

以下の組み合わせを同じ controller の HTTP サーバーと iPXE スクリプト生成で扱えるようにする。

- kubeadm + Ubuntu autoinstall
- kubeadm + Debian cloud-init
- kubeadm + Debian Installer preseed
- k3s + Ubuntu autoinstall
- k3s + Debian cloud-init
- k3s + Debian Installer preseed
- Talos

controller は kubeadm と k3s の中身を解釈しない。CAPI の bootstrap provider またはユーザーが生成した Secret の `data.value` を、選択された OS installer / datasource の形式で配信する。

## 非目的

- kubeadm bootstrap provider や k3s bootstrap provider の実装をこの変更に含めない。
- Ubuntu autoinstall YAML や Debian preseed の完全な生成器は実装しない。
- OS インストール後のディスク永続化、再起動、PXE ブート順制御はこの変更に含めない。
- Secret の `data.value` 以外の複数キーを組み合わせる汎用テンプレートエンジンは実装しない。

## API 設計

`TartMachineSpec` に bootstrap 配信形式を表す `bootstrap` を追加する。

```go
type TartMachineBootstrapFormat string

const (
	TartMachineBootstrapFormatTalos   TartMachineBootstrapFormat = "Talos"
	TartMachineBootstrapFormatNoCloud TartMachineBootstrapFormat = "NoCloud"
	TartMachineBootstrapFormatPreseed TartMachineBootstrapFormat = "Preseed"
	TartMachineBootstrapFormatRaw     TartMachineBootstrapFormat = "Raw"
)

type TartMachineBootstrapSpec struct {
	// format selects how bootstrap data is exposed to the booted OS or installer.
	// +kubebuilder:default=NoCloud
	Format TartMachineBootstrapFormat `json:"format,omitempty"`
}

type TartMachineSpec struct {
	// existing fields...
	Bootstrap TartMachineBootstrapSpec `json:"bootstrap,omitempty"`
}
```

`format` の default は Ubuntu kubeadm 向けの `NoCloud` とする。既存の `TartMachine` や sample が `bootstrap` を持たない場合は `NoCloud` として扱い、Ubuntu kubeadm の seed URL を自動生成する。`talos.config=` は `format: Talos` を明示した場合にのみ追加する。

`Raw` は controller が bootstrap 用 kernel parameter を追加しない形式とする。ユーザーは `kernelParams` へ必要な値をすべて明示する。

NoCloud では `user-data` 取得後も cloud-init が同じ seed 配下の `meta-data` / `vendor-data` を取得する可能性がある。そのため、token Secret を削除した後も同じ path token だけを非機密 metadata に許可できるよう、`TartMachineStatus` に消費済み token の SHA-256 hash を保存する。

```go
type TartMachineStatus struct {
	// existing fields...

	// consumedBootstrapTokenHash stores the SHA-256 hash of the consumed bootstrap token for non-secret NoCloud metadata validation.
	// +optional
	ConsumedBootstrapTokenHash string `json:"consumedBootstrapTokenHash,omitempty"`
}
```

この field は Secret 値そのものではなく、推測困難な one-time token の SHA-256 hash だけを保持する。`BeginProvisioningStatus` と `RetryExpiredTokenStatus` では空に戻し、`BootstrapTokenConsumedStatus` で消費した token の hash を保存する。

## iPXE スクリプト生成

`/ipxe` は TartHost の MAC address から TartMachine を引き、`spec.bootstrap.format` に応じて kernel parameter を追加する。

`Talos`:

```text
talos.config=http://<server>/metadata/<namespace>/<name>?token=<token>
```

`NoCloud`:

```text
ds=nocloud-net;s=http://<server>/metadata/<namespace>/<name>/nocloud/<token>/
```

Ubuntu autoinstall を利用する template では `kernelParams` に `autoinstall` を指定する。controller は `autoinstall` を format から暗黙追加しない。Debian cloud-init では `autoinstall` を指定しない。

`Preseed`:

```text
auto=true priority=critical url=http://<server>/metadata/<namespace>/<name>/preseed.cfg?token=<token>
```

必要に応じてユーザーは `kernelParams` で `interface=auto` や `netcfg/dhcp_timeout=60` を追加する。

`Raw`:

```text
追加なし
```

token Secret が存在しない場合は、どの format でも bootstrap 用 kernel parameter を追加しない。これは既存挙動に合わせ、bootstrap data がまだ準備されていない状態で HTTP ハンドラが失敗しないようにするためである。

## HTTP エンドポイント設計

既存の Talos / raw 単一ファイル配信を維持する。

```text
GET /metadata/:namespace/:name?token=<token>
```

成功時は CAPI Machine の `spec.bootstrap.dataSecretName` が指す Secret の `data.value` を `application/octet-stream` で返し、bootstrap token を消費する。

NoCloud 用に以下を追加する。

```text
GET /metadata/:namespace/:name/nocloud/:token/meta-data
GET /metadata/:namespace/:name/nocloud/:token/user-data
GET /metadata/:namespace/:name/nocloud/:token/vendor-data
```

`meta-data` は controller が生成する。

```yaml
instance-id: <namespace>-<name>
local-hostname: <name>
```

`user-data` は Secret の `data.value` を返す。`vendor-data` は空の cloud-config を返す。

```yaml
#cloud-config
{}
```

NoCloud は `seedfrom` が末尾 `/` の base URL であり、cloud-init はその配下の `user-data`、`meta-data`、`vendor-data`、必要に応じて `network-config` を取得する。query string に token を置くと child URL 生成が cloud-init 実装に依存するため、NoCloud の token は path segment に含める。

NoCloud の token 消費は `user-data` の正常返却時だけ行う。`meta-data` と `vendor-data` は token を検証するが消費しない。`user-data` 取得後に token Secret が削除されている場合でも、`sha256(path token)` が `status.consumedBootstrapTokenHash` と一致する場合だけ `meta-data` / `vendor-data` を返す。これにより、cloud-init の取得順が `user-data` を先に読む場合でも同じ seed URL だけを継続許可できる。

Preseed 用に以下を追加する。

```text
GET /metadata/:namespace/:name/preseed.cfg?token=<token>
```

成功時は Secret の `data.value` を `text/plain; charset=utf-8` で返し、bootstrap token を消費する。

## Token とセキュリティ

Talos と Preseed の metadata エンドポイントは `token` query parameter を必須とする。NoCloud の metadata エンドポイントは path segment の `:token` を必須とする。token は現在と同じ Secret ベースの bootstrap token service から取得し、constant time comparison で検証する。

`TokenExpiresAt` が過去の場合は metadata を返さない。機密データ本体を返す endpoint では、Secret 読み取り後に TartMachine と token を再取得してから token を消費する。これは並行リクエストで同じ token が複数回使われることを防ぐためである。

token 消費対象:

- `/metadata/:namespace/:name`
- `/metadata/:namespace/:name/nocloud/:token/user-data`
- `/metadata/:namespace/:name/preseed.cfg`

token 非消費対象:

- `/metadata/:namespace/:name/nocloud/:token/meta-data`
- `/metadata/:namespace/:name/nocloud/:token/vendor-data`

NoCloud の非消費 endpoint は、token Secret が存在する間は live token と照合する。token Secret が消費済みの場合は `status.consumedBootstrapTokenHash` と path token の SHA-256 hash を照合する。hash が空、または不一致の場合は metadata を返さない。

## 実装構成

`internal/server/ipxe/server.go` は現在 metadata の検証、Secret 解決、token 消費、レスポンス生成を 1 ファイルで抱えている。今回の変更では大規模な再配置は避けるが、重複を避けるために以下の小さな内部関数を追加する。

- `bootstrapFormat(machine *TartMachine) TartMachineBootstrapFormat`
- `buildBootstrapKernelParams(ctx, cl, serverURL, machine) ([]string, error)`
- `buildMetadataURL(serverURL, machine, token) string`
- `buildNoCloudSeedURL(serverURL, machine, token) string`
- `buildPreseedURL(serverURL, machine, token) string`
- `serveBootstrapData(c, cl, contentType, consumeToken) error`
- `serveNoCloudMetaData(c, cl) error`
- `serveNoCloudVendorData(c, cl) error`

`serveBootstrapData` は既存の `handleMetadata` の共通処理を受け持ち、Secret の `data.value` を返す endpoint で使う。

## Sample と Template

既存の `cluster-template-kubeadm.yaml` は Ubuntu kubeadm / NoCloud の default template として更新する。

```yaml
bootstrap:
  format: NoCloud
kernelParams:
- console=ttyS0
- ip=dhcp
- autoinstall
```

`BOOTSTRAP_METADATA_URL` は controller が NoCloud seed URL を自動生成するため不要にする。

追加 sample / template:

- `config/templates/cluster-template-kubeadm-ubuntu.yaml`
- `config/templates/cluster-template-kubeadm-debian.yaml`
- `config/templates/cluster-template-k3s-ubuntu.yaml`
- `config/templates/cluster-template-k3s-debian.yaml`
- `config/templates/cluster-template-talos.yaml`
- `config/samples/cluster-kubeadm-ubuntu.yaml`
- `config/samples/cluster-kubeadm-debian.yaml`
- `config/samples/cluster-k3s-ubuntu.yaml`
- `config/samples/cluster-k3s-debian.yaml`
- `config/samples/cluster-talos.yaml`

k3s の sample は CAPI bootstrap provider が未導入でも API sample として適用できる範囲に留める。bootstrap Secret の中身は k3s install 用 cloud-init または preseed をユーザーが用意する前提をコメントではなくマニフェスト構造で示す。

## E2E テスト

既存 E2E は manager 起動、metrics、`TartMachineTemplate` sample apply を確認している。今回の E2E では実機 OS インストールをしない範囲で、API と配信形式が Kubernetes 上で受け入れられることを確認する。

追加する確認:

- Ubuntu kubeadm template が `bootstrap.format=NoCloud` で apply できる。
- Debian kubeadm template が `bootstrap.format=Preseed` または `NoCloud` で apply できる。
- Ubuntu k3s sample が `bootstrap.format=NoCloud` で apply できる。
- Debian k3s sample が `bootstrap.format=Preseed` または `NoCloud` で apply できる。
- Talos sample が `bootstrap.format=Talos` で apply できる。

HTTP ハンドラの実配信と token 消費は unit test で検証する。E2E では controller Pod 内 HTTP サーバーへの完全な metadata Secret 配信までは行わない。理由は、CAPI Machine、bootstrap Secret、TartMachine、TartHost、token Secret、hostNetwork 到達性をすべて組み合わせると、実機 provisioning に近い別種の E2E になるためである。

## Unit Test

`internal/server/ipxe/server_test.go` に以下を追加する。

- `Talos` format は `talos.config=` を生成する。
- `NoCloud` format は `ds=nocloud-net;s=.../nocloud/<token>/` を生成する。
- `Preseed` format は `auto=true priority=critical url=.../preseed.cfg?token=...` を生成する。
- `Raw` format は bootstrap 用 kernel parameter を追加しない。
- NoCloud `meta-data` は token を消費しない。
- NoCloud `user-data` は Secret の `data.value` を返して token を消費する。
- NoCloud `vendor-data` は token を消費しない。
- NoCloud `user-data` 取得後も、同じ path token で `meta-data` / `vendor-data` を取得できる。
- NoCloud `user-data` 取得後、別の path token では `meta-data` / `vendor-data` を取得できない。
- `user-data` / Talos config / `preseed.cfg` の正常配信後、`status.consumedBootstrapTokenHash` に消費済み token の SHA-256 hash が保存される。
- Preseed `preseed.cfg` は Secret の `data.value` を返して token を消費する。
- token なし、invalid token、expired token は既存 endpoint と同様に拒否される。

`api/v1alpha1` の template test では `Bootstrap.Format` が `TartMachineTemplate` に保持されることを確認する。

## CRD 生成

API 型を変更するため、`controller-gen` を含む既存 mise task で CRD と deepcopy を再生成する。手動編集対象は Go 型と test / sample に限定し、生成物は generator の出力をそのまま commit する。

## 移行

`bootstrap.format` 未指定は `NoCloud` として扱うため、既存の `cluster-template-kubeadm.yaml` など Ubuntu kubeadm 前提の利用者はそのまま NoCloud を使う。一方で Talos 想定の利用者は `format: Talos` を明示する必要がある。

Ubuntu kubeadm sample は今回から NoCloud 形式に変わる。これにより `BOOTSTRAP_METADATA_URL` 変数は不要になる。既存 sample の `ds=nocloud-net;s=${BOOTSTRAP_METADATA_URL}` は controller 自動生成に置き換える。

## 受け入れ条件

- `go test ./... -v` が成功する。
- `mise run test` が成功する。
- `mise run lint` が成功する。
- CRD に `spec.bootstrap.format` が出力される。
- CRD に `status.consumedBootstrapTokenHash` が出力される。
- すべての sample / template が Kubernetes API に受け入れられる。
- Talos 既存挙動の unit test が維持される。
- NoCloud と Preseed の token 消費タイミングが unit test で固定される。
- NoCloud の `user-data` 先行取得でも同じ seed token の `meta-data` / `vendor-data` が取得でき、別 token は拒否される。

## 2026-05-05 実装引き継ぎ状況

完了済みコミット:

- `c76348c` `bootstrap metadata形式をTartMachine APIに追加`
- `6baa315` `bootstrap形式別にiPXE kernel parameterを生成`
- `d1e7073` `NoCloudとPreseedのmetadata配信を追加`
- `8d8b32e` `NoCloud metadataのtoken検証を修正`

Task 1 と Task 2 は実装・仕様レビュー・コード品質レビューが完了している。Task 3 は `d1e7073` で基本実装、`8d8b32e` で NoCloud seed URL を path token に変更したが、再レビューで「token Secret 消費後に任意 token の `meta-data` / `vendor-data` が通る」問題が残っている。

次の作業者は、現在の未コミット途中差分を確認してから継続すること。途中差分は `internal/domain/machine/state.go`、`internal/domain/machine/state_test.go`、`internal/server/ipxe/server_test.go` にあり、`ConsumedBootstrapTokenHash` 導入の一部だけが入っている。まだ API field、CRD/deepcopy、`server.go` の hash 保存・照合が未完了である。

次に完了すべき仕様:

- `TartMachineStatus.ConsumedBootstrapTokenHash` を API と CRD に追加する。
- token 消費時に `sha256(token)` の lowercase hex を status に保存する。
- NoCloud `meta-data` / `vendor-data` は live token がある場合は live token と照合し、消費済みの場合は path token の hash と `status.consumedBootstrapTokenHash` を照合する。
- NoCloud `user-data` は live token 必須のままにし、消費済み hash だけでは再配信しない。
- `go test ./internal/server/ipxe ./internal/domain/machine ./api/v1alpha1 -v` と `mise run generate` / `mise run manifests` を通す。

## 参考

- cloud-init NoCloud datasource: `https://docs.cloud-init.io/en/latest/reference/datasources/nocloud.html`
- Debian Installer preseed appendix: `https://www.debian.org/releases/stable/amd64/apb.en.html`
