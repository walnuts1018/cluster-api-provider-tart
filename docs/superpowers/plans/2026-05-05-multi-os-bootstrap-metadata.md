# Multi OS Bootstrap Metadata Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Talos、Ubuntu/Debian NoCloud、Debian preseed の bootstrap metadata 配信を `TartMachine.spec.bootstrap.format` で切り替え、kubeadm/k3s 向け sample と E2E 検証を追加する。

**Architecture:** `TartMachineSpec` に bootstrap format を追加し、HTTP server は format に応じた iPXE kernel parameter と metadata endpoint を提供する。Bootstrap Secret の `data.value` は controller が解釈せず、Talos config、cloud-init user-data、preseed.cfg として配信する。

**Tech Stack:** Go、Kubebuilder/controller-gen、controller-runtime fake client、Echo、Cluster API v1beta2 manifests、mise tasks。

---

## 2026-05-05 Handoff Status

このブランチでは以下のコミットまで完了している。

- `2c36589` `multi OS bootstrap metadataの設計を追加`
- `4dd7d39` `multi OS bootstrap metadataの実装計画を追加`
- `c76348c` `bootstrap metadata形式をTartMachine APIに追加`
- `6baa315` `bootstrap形式別にiPXE kernel parameterを生成`
- `d1e7073` `NoCloudとPreseedのmetadata配信を追加`
- `8d8b32e` `NoCloud metadataのtoken検証を修正`

Task 1 と Task 2 は仕様レビュー、コード品質レビューともに承認済み。Task 3 は `d1e7073` の後に NoCloud の取得順問題が見つかり、`8d8b32e` で query token から path token へ修正した。ただし再仕様レビューで、token Secret 消費後に任意の path token でも `meta-data` / `vendor-data` が取れる問題が残っていると判定された。

現在の作業ツリーには、停止した subagent による未コミット途中差分が残っている。revert せず内容を確認してから継続すること。

- Modified: `internal/domain/machine/state.go`
- Modified: `internal/domain/machine/state_test.go`
- Modified: `internal/server/ipxe/server_test.go`

途中差分の意図:

- `BootstrapTokenConsumedStatus` に `consumedTokenHash` 引数を追加する。
- `BeginProvisioningStatus` / `RetryExpiredTokenStatus` で `ConsumedBootstrapTokenHash` を空に戻す。
- domain test と iPXE metadata test に consumed hash の期待値を追加する。

途中差分はまだ不完全で、`TartMachineStatus.ConsumedBootstrapTokenHash` API field、deepcopy、CRD、`server.go` の hash 保存・照合実装が未完了。現時点ではこの差分のまま test を実行すると compile failure になる可能性が高い。

次に行うべきこと:

1. 途中差分を読み、採用する形で Task 3b を完了させる。
2. `TartMachineStatus.ConsumedBootstrapTokenHash` を追加し、`mise run generate` / `mise run manifests` を実行する。
3. NoCloud `user-data` 消費時に SHA-256 hex hash を status に保存する。
4. NoCloud `meta-data` / `vendor-data` は、live token がある間は live token を検証し、token Secret 消費後は `status.consumedBootstrapTokenHash` と path token の SHA-256 hash を constant time compare する。
5. `user-data` 取得後に同じ token の `meta-data` / `vendor-data` が 200、別 token が 403 になる test を追加する。
6. Task 3 のレビューを再実行する。

---

## File Structure

- Modify: `api/v1alpha1/tartmachine_types.go`
  - `TartMachineBootstrapFormat`、`TartMachineBootstrapSpec`、`TartMachineSpec.Bootstrap`、`TartMachineStatus.ConsumedBootstrapTokenHash` を追加する。
- Modify: `api/v1alpha1/tartmachine_template_types_test.go`
  - template が `Bootstrap.Format` を保持する test を追加する。
- Generated: `api/v1alpha1/zz_generated.deepcopy.go`
  - `mise run generate` で更新する。
- Generated: `config/crd/bases/infrastructure.cluster.x-k8s.io_tartmachines.yaml`
  - `mise run manifests` で `spec.bootstrap.format` を出力する。
- Generated: `config/crd/bases/infrastructure.cluster.x-k8s.io_tartmachinetemplates.yaml`
  - `mise run manifests` で template 側の `spec.template.spec.bootstrap.format` を出力する。
- Modify: `internal/server/ipxe/server.go`
  - format 別の kernel parameter 生成と metadata endpoint を追加する。
- Modify: `internal/server/ipxe/server_test.go`
  - iPXE script、NoCloud endpoint、Preseed endpoint、token 消費を TDD で固定する。
- Modify: `internal/domain/machine/state.go`
  - 消費済み bootstrap token hash を status に保存し、再 provisioning 時にクリアする。
- Modify: `internal/domain/machine/state_test.go`
  - token hash の保存とクリアを検証する。
- Modify: `config/templates/cluster-template-kubeadm.yaml`
  - 既存 template を Ubuntu NoCloud 形式へ更新する。
- Create: `config/templates/cluster-template-kubeadm-ubuntu.yaml`
  - `cluster-template-kubeadm.yaml` と同じ内容を明示名で提供する。
- Create: `config/templates/cluster-template-kubeadm-debian.yaml`
  - Debian preseed 形式の kubeadm template を追加する。
- Create: `config/templates/cluster-template-k3s-ubuntu.yaml`
  - Ubuntu NoCloud 形式の k3s infrastructure sample template を追加する。
- Create: `config/templates/cluster-template-k3s-debian.yaml`
  - Debian preseed 形式の k3s infrastructure sample template を追加する。
- Create: `config/templates/cluster-template-talos.yaml`
  - Talos 形式の template を追加する。
- Modify/Create: `config/samples/*.yaml`
  - kubeadm/k3s/Talos の sample を追加し、`config/samples/kustomization.yaml` に含める。
- Modify: `test/templates/kubeadm_template_test.go`
  - template ファイル群の kind と bootstrap format を検証する。
- Modify: `test/e2e/e2e_test.go`
  - sample/template apply と `bootstrap.format` の保存確認を追加する。
- Modify: `test/e2e/config/tart.yaml`
  - `BOOTSTRAP_METADATA_URL` を削除し、必要な OS image 変数を追加する。

---

### Task 1: API に bootstrap format を追加

**Files:**

- Modify: `api/v1alpha1/tartmachine_types.go`
- Modify: `api/v1alpha1/tartmachine_template_types_test.go`
- Generated later: `api/v1alpha1/zz_generated.deepcopy.go`
- Generated later: `config/crd/bases/infrastructure.cluster.x-k8s.io_tartmachines.yaml`
- Generated later: `config/crd/bases/infrastructure.cluster.x-k8s.io_tartmachinetemplates.yaml`

- [ ] **Step 1: failing test を追加する**

`api/v1alpha1/tartmachine_template_types_test.go` の `TestTartMachineTemplateCarriesTartMachineSpec` 内の `Spec` に `Bootstrap` を追加し、末尾に検証を追加する。

```go
Spec: TartMachineSpec{
 Image:        "https://assets.hoge.test.walnuts.dev/ubuntu/vmlinuz",
 Initrd:       "https://assets.hoge.test.walnuts.dev/ubuntu/initrd",
 KernelParams: []string{"console=ttyS0", "autoinstall"},
 Bootstrap: TartMachineBootstrapSpec{
  Format: TartMachineBootstrapFormatNoCloud,
 },
},
```

```go
if got, want := template.Spec.Template.Spec.Bootstrap.Format, TartMachineBootstrapFormatNoCloud; got != want {
 t.Fatalf("bootstrap.format = %q, want %q", got, want)
}
```

- [ ] **Step 2: failing test を確認する**

Run:

```bash
go test ./api/v1alpha1 -run TestTartMachineTemplateCarriesTartMachineSpec -v
```

Expected: `undefined: TartMachineBootstrapSpec` または `undefined: TartMachineBootstrapFormatNoCloud` で失敗する。

- [ ] **Step 3: API 型を実装する**

`api/v1alpha1/tartmachine_types.go` の `TartMachineSpec` より上に以下を追加する。

```go
// TartMachineBootstrapFormat selects how bootstrap data is exposed to the booted OS or installer.
type TartMachineBootstrapFormat string

const (
 // TartMachineBootstrapFormatTalos serves bootstrap data as a single Talos machine config.
 TartMachineBootstrapFormatTalos TartMachineBootstrapFormat = "Talos"
 // TartMachineBootstrapFormatNoCloud serves bootstrap data through cloud-init NoCloud files.
 TartMachineBootstrapFormatNoCloud TartMachineBootstrapFormat = "NoCloud"
 // TartMachineBootstrapFormatPreseed serves bootstrap data as a Debian Installer preseed file.
 TartMachineBootstrapFormatPreseed TartMachineBootstrapFormat = "Preseed"
 // TartMachineBootstrapFormatRaw leaves bootstrap kernel parameters fully user-managed.
 TartMachineBootstrapFormatRaw TartMachineBootstrapFormat = "Raw"
)

// TartMachineBootstrapSpec defines how bootstrap data is served to the machine.
type TartMachineBootstrapSpec struct {
// format selects how bootstrap data is exposed to the booted OS or installer.
// Defaults to NoCloud when omitted.
// +optional
// +kubebuilder:default=NoCloud
// +kubebuilder:validation:Enum=Talos;NoCloud;Preseed;Raw
Format TartMachineBootstrapFormat `json:"format,omitempty"`
}
```

`TartMachineSpec` の `Initrd` の後に追加する。

```go
// bootstrap configures how bootstrap data is passed to the booted OS or installer.
// +optional
// +kubebuilder:default={}
Bootstrap TartMachineBootstrapSpec `json:"bootstrap,omitempty"`
```

- [ ] **Step 4: API test を通す**

Run:

```bash
go test ./api/v1alpha1 -run TestTartMachineTemplateCarriesTartMachineSpec -v
```

Expected: PASS。

- [ ] **Step 5: 生成物を更新する**

Run:

```bash
mise run generate
mise run manifests
```

Expected: `api/v1alpha1/zz_generated.deepcopy.go` と CRD YAML が更新される。

- [ ] **Step 6: CRD に field が出たことを確認する**

Run:

```bash
rg -n "bootstrap:|format:|NoCloud|Preseed|Raw" config/crd/bases/infrastructure.cluster.x-k8s.io_tartmachines.yaml config/crd/bases/infrastructure.cluster.x-k8s.io_tartmachinetemplates.yaml
```

Expected: `bootstrap`、`format`、`default: NoCloud`、`enum` に `Talos`、`NoCloud`、`Preseed`、`Raw` が出る。

- [ ] **Step 7: コミットする**

Run:

```bash
git status --short
git --no-pager add api/v1alpha1/tartmachine_types.go api/v1alpha1/tartmachine_template_types_test.go api/v1alpha1/zz_generated.deepcopy.go config/crd/bases/infrastructure.cluster.x-k8s.io_tartmachines.yaml config/crd/bases/infrastructure.cluster.x-k8s.io_tartmachinetemplates.yaml
git --no-pager commit --signoff -m "bootstrap metadata形式をTartMachine APIに追加"
```

---

### Task 2: iPXE script の format 別 kernel parameter を実装

**Files:**

- Modify: `internal/server/ipxe/server_test.go`
- Modify: `internal/server/ipxe/server.go`

- [ ] **Step 1: failing tests を追加する**

`internal/server/ipxe/server_test.go` の `TestHandlerDynamicScript` に、NoCloud、Preseed、Raw の machine/host/token を追加する。既存 `ValidRequest_MACAddress` は NoCloud default の後方互換 test として残す。

追加する test case の期待値:

```go
if !strings.Contains(body, "ds=nocloud-net;s=http://bootstrap.example.invalid/metadata/default/test-machine-nocloud/nocloud/"+token+"/") {
 t.Errorf("body missing NoCloud seed URL: %s", body)
}
```

```go
if !strings.Contains(body, "auto=true priority=critical url=http://bootstrap.example.invalid/metadata/default/test-machine-preseed/preseed.cfg?token="+token) {
 t.Errorf("body missing preseed URL: %s", body)
}
```

```go
if strings.Contains(body, "talos.config=") || strings.Contains(body, "ds=nocloud-net") || strings.Contains(body, "preseed.cfg") {
 t.Errorf("raw format unexpectedly added bootstrap params: %s", body)
}
```

- [ ] **Step 2: failing tests を確認する**

Run:

```bash
go test ./internal/server/ipxe -run TestHandlerDynamicScript -v
```

Expected: NoCloud / Preseed / Raw の期待値が満たされず失敗する。

- [ ] **Step 3: URL builder と format resolver を実装する**

`internal/server/ipxe/server.go` の `buildMetadataURL` を token 取得と URL 生成に分ける。

```go
func bootstrapFormat(machine *infrastructurev1alpha1.TartMachine) infrastructurev1alpha1.TartMachineBootstrapFormat {
 if machine.Spec.Bootstrap.Format == "" {
  return infrastructurev1alpha1.TartMachineBootstrapFormatTalos
 }
 return machine.Spec.Bootstrap.Format
}

func buildMetadataURL(serverURL string, machine *infrastructurev1alpha1.TartMachine, token string) string {
 metadataPath := fmt.Sprintf("/metadata/%s/%s", url.PathEscape(machine.Namespace), url.PathEscape(machine.Name))
 return fmt.Sprintf("%s%s?token=%s", serverURL, metadataPath, url.QueryEscape(token))
}

func buildNoCloudSeedURL(serverURL string, machine *infrastructurev1alpha1.TartMachine, token string) string {
 metadataPath := fmt.Sprintf("/metadata/%s/%s/nocloud/%s/", url.PathEscape(machine.Namespace), url.PathEscape(machine.Name), url.PathEscape(token))
 return serverURL + metadataPath
}

func buildPreseedURL(serverURL string, machine *infrastructurev1alpha1.TartMachine, token string) string {
 metadataPath := fmt.Sprintf("/metadata/%s/%s/preseed.cfg", url.PathEscape(machine.Namespace), url.PathEscape(machine.Name))
 return fmt.Sprintf("%s%s?token=%s", serverURL, metadataPath, url.QueryEscape(token))
}
```

- [ ] **Step 4: kernel parameter 生成を実装する**

`generateIPXEScript` の metadata URL 生成を以下の形に変更する。

```go
bootstrapParams, err := buildBootstrapKernelParams(c.Request().Context(), cl, serverURL, machine)
if err != nil {
 return "", err
}
paramsList := append([]string{}, machine.Spec.KernelParams...)
paramsList = append(paramsList, bootstrapParams...)
params := strings.Join(paramsList, " ")
```

追加する関数:

```go
func buildBootstrapKernelParams(ctx context.Context, cl client.Client, serverURL string, machine *infrastructurev1alpha1.TartMachine) ([]string, error) {
 token, exists, err := k8sbootstraptoken.NewService(cl).Get(ctx, machine)
 if err != nil {
  return nil, err
 }
 if !exists {
  return nil, nil
 }

 switch bootstrapFormat(machine) {
 case infrastructurev1alpha1.TartMachineBootstrapFormatTalos:
  return []string{"talos.config=" + buildMetadataURL(serverURL, machine, token.String())}, nil
 case infrastructurev1alpha1.TartMachineBootstrapFormatNoCloud:
  return []string{"ds=nocloud-net;s=" + buildNoCloudSeedURL(serverURL, machine, token.String())}, nil
 case infrastructurev1alpha1.TartMachineBootstrapFormatPreseed:
  return []string{"auto=true", "priority=critical", "url=" + buildPreseedURL(serverURL, machine, token.String())}, nil
 case infrastructurev1alpha1.TartMachineBootstrapFormatRaw:
  return nil, nil
 default:
  return nil, fmt.Errorf("unsupported bootstrap format: %s", machine.Spec.Bootstrap.Format)
 }
}
```

- [ ] **Step 5: tests を通す**

Run:

```bash
go test ./internal/server/ipxe -run TestHandlerDynamicScript -v
```

Expected: PASS。

- [ ] **Step 6: コミットする**

Run:

```bash
git status --short
git --no-pager add internal/server/ipxe/server.go internal/server/ipxe/server_test.go
git --no-pager commit --signoff -m "bootstrap形式別にiPXE kernel parameterを生成"
```

---

### Task 3: NoCloud と Preseed の metadata endpoint を実装

**Files:**

- Modify: `internal/server/ipxe/server_test.go`
- Modify: `internal/server/ipxe/server.go`

- [ ] **Step 1: NoCloud metadata の failing tests を追加する**

`TestHandlerServesMetadata` に以下の test を追加する。NoCloud は cloud-init の `seedfrom` として末尾 `/` の base URL を渡すため、token は query ではなく path segment に置く。

まず test helper を追加する。

```go
func metadataObjects(machineName, ownerName, secretName, token string, expiresAt time.Time) (*infrastructurev1alpha1.TartMachine, *unstructured.Unstructured, *corev1.Secret, *corev1.Secret) {
 tartMachine := &infrastructurev1alpha1.TartMachine{
  ObjectMeta: metav1.ObjectMeta{
   Name:       machineName,
   Namespace:  "default",
   Generation: 3,
   OwnerReferences: []metav1.OwnerReference{
    {
     APIVersion: "cluster.x-k8s.io/v1beta1",
     Kind:       "Machine",
     Name:       ownerName,
    },
   },
  },
  Status: infrastructurev1alpha1.TartMachineStatus{
   HostRef: &corev1.ObjectReference{
    Name:      "test-host",
    Namespace: "default",
   },
   ProvisioningStartTime: &metav1.Time{Time: expiresAt.Add(-10 * time.Minute)},
   TokenExpiresAt:        &metav1.Time{Time: expiresAt},
  },
 }
 capiMachine := &unstructured.Unstructured{
  Object: map[string]any{
   "apiVersion": "cluster.x-k8s.io/v1beta1",
   "kind":       "Machine",
   "metadata": map[string]any{
    "name":      ownerName,
    "namespace": "default",
   },
   "spec": map[string]any{
    "bootstrap": map[string]any{
     "dataSecretName": secretName,
    },
   },
  },
 }
 bootstrapSecret := &corev1.Secret{
  ObjectMeta: metav1.ObjectMeta{
   Name:      secretName,
   Namespace: "default",
  },
  Data: map[string][]byte{
   "value": []byte("bootstrap-config"),
  },
 }
 tokenSecret := &corev1.Secret{
  ObjectMeta: metav1.ObjectMeta{
   Name:      machineName + "-bootstrap-token",
   Namespace: "default",
  },
  Data: map[string][]byte{
   "token": []byte(token),
  },
 }
 return tartMachine, capiMachine, bootstrapSecret, tokenSecret
}
```

```go
t.Run("NoCloudMetaDataDoesNotConsumeToken", func(t *testing.T) {
 farFuture := metav1.Now().Add(1 * time.Hour)
 tartMachine, capiMachine, bootstrapSecret, tokenSecret := metadataObjects("test-machine", "capi-machine", "bootstrap-secret", token, farFuture)
 cl := setupFakeClient(t, s, tartMachine, capiMachine, bootstrapSecret, tokenSecret)

 req := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine/nocloud/"+token+"/meta-data", nil)
 rec := httptest.NewRecorder()
 ipxe.NewHandler(cl, ipxe.HandlerConfig{}).ServeHTTP(rec, req)

 if rec.Code != http.StatusOK {
  t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
 }
 if body := rec.Body.String(); !strings.Contains(body, "instance-id: default-test-machine") || !strings.Contains(body, "local-hostname: test-machine") {
  t.Fatalf("unexpected meta-data body: %q", body)
 }
 remainingSecret := &corev1.Secret{}
 if err := cl.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "test-machine-bootstrap-token"}, remainingSecret); err != nil {
  t.Fatalf("bootstrap token secret was consumed: %v", err)
 }
})
```

同じ helper を使って `NoCloudUserDataConsumesToken`、`NoCloudVendorDataDoesNotConsumeToken`、`PreseedConsumesToken` を追加する。期待値は以下。

```go
// user-data
body == "bootstrap-config"
content type contains "text/cloud-config"
token Secret is not found after delivery
TartMachine status.consumedBootstrapTokenHash == sha256(token) as lowercase hex
```

```go
// vendor-data
body == "#cloud-config\n{}\n"
token Secret still exists
```

```go
// preseed.cfg
body == "bootstrap-config"
content type contains "text/plain"
token Secret is not found after delivery
```

- [ ] **Step 2: failing tests を確認する**

Run:

```bash
go test ./internal/server/ipxe -run TestHandlerServesMetadata -v
```

Expected: 追加 endpoint が 404 で失敗する。

- [ ] **Step 3: route を追加する**

`NewHandler` の metadata route 登録に NoCloud / Preseed route を追加する。rate limiter がある場合も同じ limiter を通す。

```go
registerMetadataRoutes(e, cl, config.MetadataLimiter)
```

追加する helper:

```go
func registerMetadataRoutes(e *echo.Echo, cl client.Client, limiter *rate.Limiter) {
 withLimit := func(next func(c *echo.Context) error) func(c *echo.Context) error {
  return func(c *echo.Context) error {
   if limiter != nil && !limiter.Allow() {
    return c.String(http.StatusTooManyRequests, "rate limit exceeded")
   }
   return next(c)
  }
 }
 e.GET("/metadata/:namespace/:name", withLimit(func(c *echo.Context) error {
  return serveBootstrapData(c, cl, "application/octet-stream", true)
 }))
 e.GET("/metadata/:namespace/:name/nocloud/:token/meta-data", withLimit(func(c *echo.Context) error {
  return serveNoCloudMetaData(c, cl)
 }))
 e.GET("/metadata/:namespace/:name/nocloud/:token/user-data", withLimit(func(c *echo.Context) error {
  return serveBootstrapData(c, cl, "text/cloud-config; charset=utf-8", true)
 }))
 e.GET("/metadata/:namespace/:name/nocloud/:token/vendor-data", withLimit(func(c *echo.Context) error {
  return serveNoCloudVendorData(c, cl)
 }))
 e.GET("/metadata/:namespace/:name/preseed.cfg", withLimit(func(c *echo.Context) error {
  return serveBootstrapData(c, cl, "text/plain; charset=utf-8", true)
 }))
}
```

- [ ] **Step 4: token 検証と Secret 配信を共通化する**

既存 `handleMetadata` の本体を `serveBootstrapData` に置き換える。`consumeToken` が `false` の場合は再取得と `consumeBootstrapToken` を実行しない。

```go
func serveBootstrapData(c *echo.Context, cl client.Client, contentType string, consumeToken bool) error {
 ctx, span := telemetry.Tracer.Start(c.Request().Context(), "Metadata.Get")
 defer span.End()

 machine, tokenService, providedToken, err := validateMetadataRequest(c, cl)
 if err != nil {
  return err
 }

 secretName, err := bootstrapDataSecretName(ctx, cl, machine)
 if err != nil {
  if apierrors.IsNotFound(err) {
   span.SetStatus(codes.Error, "owner not found")
   return c.String(http.StatusNotFound, "bootstrap secret owner Machine not found")
  }
  span.RecordError(err)
  span.SetStatus(codes.Error, err.Error())
  return c.String(http.StatusPreconditionFailed, err.Error())
 }

 var secret corev1.Secret
 if err := cl.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: secretName}, &secret); err != nil {
  if apierrors.IsNotFound(err) {
   span.SetStatus(codes.Error, "secret not found")
   return c.String(http.StatusNotFound, "bootstrap secret not found")
  }
  span.RecordError(err)
  span.SetStatus(codes.Error, err.Error())
  return c.String(http.StatusInternalServerError, "failed to get bootstrap secret")
 }

 data, ok := secret.Data["value"]
 if !ok {
  span.SetStatus(codes.Error, "secret missing value key")
  return c.String(http.StatusPreconditionFailed, "bootstrap secret does not contain value key")
 }

 if consumeToken {
  if err := consumeMetadataToken(c, cl, tokenService, providedToken, machine); err != nil {
   return err
  }
 }

 span.SetStatus(codes.Ok, "bootstrap data served")
 return c.Blob(http.StatusOK, contentType, data)
}
```

`handleMetadata` は削除するか、以下の薄い wrapper にする。

```go
func handleMetadata(c *echo.Context, cl client.Client) error {
 return serveBootstrapData(c, cl, "application/octet-stream", true)
}
```

- [ ] **Step 5: NoCloud 非消費 endpoint を実装する**

```go
func serveNoCloudMetaData(c *echo.Context, cl client.Client) error {
 machine, _, _, err := validateMetadataRequest(c, cl)
 if err != nil {
  return err
 }
 body := fmt.Sprintf("instance-id: %s-%s\nlocal-hostname: %s\n", machine.Namespace, machine.Name, machine.Name)
 return c.Blob(http.StatusOK, "text/yaml; charset=utf-8", []byte(body))
}

func serveNoCloudVendorData(c *echo.Context, cl client.Client) error {
 if _, _, _, err := validateMetadataRequest(c, cl); err != nil {
  return err
 }
 return c.Blob(http.StatusOK, "text/cloud-config; charset=utf-8", []byte("#cloud-config\n{}\n"))
}
```

`validateMetadataRequest` は Secret を読まずに token 検証までを行う。

```go
func validateMetadataRequest(c *echo.Context, cl client.Client) (*infrastructurev1alpha1.TartMachine, *k8sbootstraptoken.Service, string, error) {
 ctx := c.Request().Context()
 providedToken := c.QueryParam("token")
 if providedToken == "" {
  return nil, nil, "", c.String(http.StatusUnauthorized, "token is required")
 }

 var machine infrastructurev1alpha1.TartMachine
 if err := cl.Get(ctx, client.ObjectKey{Namespace: c.Param("namespace"), Name: c.Param("name")}, &machine); err != nil {
  if apierrors.IsNotFound(err) {
   return nil, nil, "", c.String(http.StatusNotFound, "TartMachine not found")
  }
  return nil, nil, "", c.String(http.StatusInternalServerError, "failed to get TartMachine")
 }

 tokenService := k8sbootstraptoken.NewService(cl)
 expectedToken, exists, err := tokenService.Get(ctx, &machine)
 if err != nil {
  return nil, nil, "", c.String(http.StatusInternalServerError, "failed to get bootstrap token")
 }
 if !exists {
  return nil, nil, "", c.String(http.StatusPreconditionFailed, "bootstrap token is not set")
 }
 if subtle.ConstantTimeCompare([]byte(providedToken), []byte(expectedToken.String())) != 1 {
  return nil, nil, "", c.String(http.StatusUnauthorized, "invalid or missing token")
 }

 now := metav1.NewTime(time.Now())
 if machine.Status.TokenExpiresAt != nil && machine.Status.TokenExpiresAt.Before(&now) {
  return nil, nil, "", c.String(http.StatusNotFound, "token has expired")
 }
 return &machine, tokenService, providedToken, nil
}
```

`consumeMetadataToken` は既存の二重消費防止処理を分離する。

```go
func consumeMetadataToken(c *echo.Context, cl client.Client, tokenService *k8sbootstraptoken.Service, providedToken string, machine *infrastructurev1alpha1.TartMachine) error {
 ctx := c.Request().Context()
 if err := cl.Get(ctx, client.ObjectKey{Namespace: c.Param("namespace"), Name: c.Param("name")}, machine); err != nil {
  if apierrors.IsNotFound(err) {
   return c.String(http.StatusNotFound, "TartMachine not found")
  }
  return c.String(http.StatusInternalServerError, "failed to get TartMachine")
 }

 expectedToken, exists, err := tokenService.Get(ctx, machine)
 if err != nil {
  return c.String(http.StatusInternalServerError, "failed to get bootstrap token")
 }
 if !exists || subtle.ConstantTimeCompare([]byte(providedToken), []byte(expectedToken.String())) != 1 {
  return c.String(http.StatusForbidden, "bootstrap token has already been consumed")
 }

 if err := consumeBootstrapToken(ctx, cl, machine); err != nil {
  if apierrors.IsConflict(err) {
   return c.String(http.StatusForbidden, "bootstrap token has already been consumed")
  }
  return c.String(http.StatusInternalServerError, "failed to consume bootstrap token")
 }
 return nil
}
```

- [ ] **Step 6: tests を通す**

Run:

```bash
go test ./internal/server/ipxe -run TestHandlerServesMetadata -v
```

Expected: PASS。

- [ ] **Step 7: コミットする**

Run:

```bash
git status --short
git --no-pager add internal/server/ipxe/server.go internal/server/ipxe/server_test.go
git --no-pager commit --signoff -m "NoCloudとPreseedのmetadata配信を追加"
```

---

### Task 3b: 消費済み NoCloud token の hash 検証を完了

**Files:**

- Modify: `api/v1alpha1/tartmachine_types.go`
- Generated: `api/v1alpha1/zz_generated.deepcopy.go`
- Generated: `config/crd/bases/infrastructure.cluster.x-k8s.io_tartmachines.yaml`
- Generated: `config/crd/bases/infrastructure.cluster.x-k8s.io_tartmachinetemplates.yaml`
- Modify: `internal/domain/machine/state.go`
- Modify: `internal/domain/machine/state_test.go`
- Modify: `internal/server/ipxe/server.go`
- Modify: `internal/server/ipxe/server_test.go`

**Context:** `d1e7073` と `8d8b32e` の Task 3 実装は、NoCloud の取得順問題を修正する途中で止まっている。現在の未コミット差分には domain 側の一部変更があるが、API field と `server.go` 実装が未完了である。差分を捨てず、採用する形で完成させる。

- [ ] **Step 1: 途中差分を確認する**

Run:

```bash
git status --short
git --no-pager diff -- internal/domain/machine/state.go internal/domain/machine/state_test.go internal/server/ipxe/server_test.go
```

Expected: `ConsumedBootstrapTokenHash` を使う途中差分が表示される。まだ `api/v1alpha1/tartmachine_types.go` には field がない。

- [ ] **Step 2: failing tests を確認する**

Run:

```bash
go test ./internal/domain/machine ./internal/server/ipxe -v
```

Expected: `ConsumedBootstrapTokenHash` field 未定義、または `BootstrapTokenConsumedStatus` signature 不一致で失敗する。

- [ ] **Step 3: API field を追加する**

`api/v1alpha1/tartmachine_types.go` の `TartMachineStatus` に追加する。

```go
// consumedBootstrapTokenHash stores the SHA-256 hash of the consumed bootstrap token for non-secret NoCloud metadata validation.
// +optional
ConsumedBootstrapTokenHash string `json:"consumedBootstrapTokenHash,omitempty"`
```

- [ ] **Step 4: domain state を完成させる**

途中差分の意図を維持する。

```go
func BootstrapTokenConsumedStatus(machine *infrastructurev1alpha1.TartMachine, consumedTokenHash string) (infrastructurev1alpha1.TartMachineStatus, error) {
 if err := validateProvisioningMachineStatus(machine.Status); err != nil {
  return infrastructurev1alpha1.TartMachineStatus{}, err
 }

 status := machine.Status.DeepCopy()
 status.TokenExpiresAt = nil
 status.ConsumedBootstrapTokenHash = consumedTokenHash
 status.ObservedGeneration = machine.Generation
 return *status, nil
}
```

`BeginProvisioningStatus` と `RetryExpiredTokenStatus` では `status.ConsumedBootstrapTokenHash = ""` を設定する。

- [ ] **Step 5: server 側の hash helper と消費処理を追加する**

`internal/server/ipxe/server.go` に `crypto/sha256` と `encoding/hex` を import し、helper を追加する。

```go
func bootstrapTokenHash(token string) string {
 sum := sha256.Sum256([]byte(token))
 return hex.EncodeToString(sum[:])
}
```

`consumeBootstrapToken` またはその呼び出し元を変更し、消費時に hash を status に保存する。

```go
status, err := machinedomain.BootstrapTokenConsumedStatus(machine, bootstrapTokenHash(providedToken))
```

- [ ] **Step 6: NoCloud 非機密 endpoint の消費後検証を実装する**

NoCloud `meta-data` / `vendor-data` 用の検証は以下の contract にする。

```go
func validateNoCloudMetadataRequest(c *echo.Context, cl client.Client) (*infrastructurev1alpha1.TartMachine, error) {
 pathToken := c.Param("token")
 if pathToken == "" {
  return nil, echo.NewHTTPError(http.StatusUnauthorized, "token is required")
 }

 var machine infrastructurev1alpha1.TartMachine
 if err := cl.Get(c.Request().Context(), client.ObjectKey{Namespace: c.Param("namespace"), Name: c.Param("name")}, &machine); err != nil {
  // existing English responses
 }

 tokenService := k8sbootstraptoken.NewService(cl)
 expectedToken, exists, err := tokenService.Get(c.Request().Context(), &machine)
 if err != nil {
  return nil, err
 }
 if exists {
  if subtle.ConstantTimeCompare([]byte(pathToken), []byte(expectedToken.String())) != 1 {
   return nil, echo.NewHTTPError(http.StatusUnauthorized, "invalid or missing token")
  }
  return &machine, nil
 }

 if machine.Status.ConsumedBootstrapTokenHash == "" {
  return nil, echo.NewHTTPError(http.StatusForbidden, "bootstrap token has already been consumed")
 }
 if subtle.ConstantTimeCompare([]byte(bootstrapTokenHash(pathToken)), []byte(machine.Status.ConsumedBootstrapTokenHash)) != 1 {
  return nil, echo.NewHTTPError(http.StatusForbidden, "bootstrap token has already been consumed")
 }
 return &machine, nil
}
```

`user-data` は live token 必須のままにする。消費済み hash だけでは `user-data` を再配信しない。

- [ ] **Step 7: tests を追加・更新する**

`internal/server/ipxe/server_test.go` に以下を固定する。

```go
t.Run("NoCloudUserDataThenMetaDataStillWorks", ...)
t.Run("NoCloudUserDataThenVendorDataStillWorks", ...)
t.Run("NoCloudUserDataThenMetaDataRejectsDifferentToken", ...)
```

期待:

- `user-data` 取得後、token Secret は削除される。
- `TartMachine.Status.ConsumedBootstrapTokenHash == sha256(token)`。
- 同じ token の `meta-data` と `vendor-data` は `200`。
- 別 token の `meta-data` は `403`。

`internal/domain/machine/state_test.go` では以下を確認する。

- `BeginProvisioningStatus` は consumed hash を空にする。
- `RetryExpiredTokenStatus` は consumed hash を空にする。
- `BootstrapTokenConsumedStatus(machine, hash)` は `TokenExpiresAt` を nil にし、hash を保存する。

- [ ] **Step 8: 生成物を更新する**

Run:

```bash
mise run generate
mise run manifests
```

Expected: deepcopy と CRD に `consumedBootstrapTokenHash` が出る。

- [ ] **Step 9: tests を通す**

Run:

```bash
go test ./internal/server/ipxe ./internal/domain/machine ./api/v1alpha1 -v
go test ./internal/server/ipxe -run 'TestHandlerDynamicScript|TestHandlerServesMetadata' -v
```

Expected: PASS。

- [ ] **Step 10: コミットする**

Run:

```bash
git status --short
git --no-pager add api/v1alpha1/tartmachine_types.go api/v1alpha1/zz_generated.deepcopy.go config/crd/bases/infrastructure.cluster.x-k8s.io_tartmachines.yaml config/crd/bases/infrastructure.cluster.x-k8s.io_tartmachinetemplates.yaml internal/domain/machine/state.go internal/domain/machine/state_test.go internal/server/ipxe/server.go internal/server/ipxe/server_test.go
git --no-pager commit --signoff -m "消費済みNoCloud tokenの検証を追加"
```

---

### Task 4: Sample と cluster template を multi OS bootstrap 用に更新

**Files:**

- Modify: `config/templates/cluster-template-kubeadm.yaml`
- Create: `config/templates/cluster-template-kubeadm-ubuntu.yaml`
- Create: `config/templates/cluster-template-kubeadm-debian.yaml`
- Create: `config/templates/cluster-template-k3s-ubuntu.yaml`
- Create: `config/templates/cluster-template-k3s-debian.yaml`
- Create: `config/templates/cluster-template-talos.yaml`
- Modify: `config/samples/infrastructure_v1alpha1_tartmachinetemplate.yaml`
- Create: `config/samples/cluster-kubeadm-ubuntu.yaml`
- Create: `config/samples/cluster-kubeadm-debian.yaml`
- Create: `config/samples/cluster-k3s-ubuntu.yaml`
- Create: `config/samples/cluster-k3s-debian.yaml`
- Create: `config/samples/cluster-talos.yaml`
- Modify: `config/samples/kustomization.yaml`
- Modify: `test/templates/kubeadm_template_test.go`
- Modify: `test/e2e/config/tart.yaml`

- [ ] **Step 1: template tests を先に追加する**

`test/templates/kubeadm_template_test.go` を table driven に変更し、複数 template を読む。

```go
func TestClusterTemplatesContainRequiredKinds(t *testing.T) {
 tests := []struct {
  name          string
  path          string
  requiredKinds []string
 }{
  {
   name: "kubeadm ubuntu",
   path: filepath.Join("..", "..", "config", "templates", "cluster-template-kubeadm-ubuntu.yaml"),
   requiredKinds: []string{"Cluster", "KubeadmControlPlane", "KubeadmConfigTemplate", "MachineDeployment", "TartCluster", "TartMachineTemplate"},
  },
  {
   name: "talos",
   path: filepath.Join("..", "..", "config", "templates", "cluster-template-talos.yaml"),
   requiredKinds: []string{"Cluster", "MachineDeployment", "TartCluster", "TartMachineTemplate"},
  },
 }
 for _, tt := range tests {
  t.Run(tt.name, func(t *testing.T) {
   found := readTemplateKinds(t, tt.path)
   for _, kind := range tt.requiredKinds {
    if !found[kind] {
     t.Fatalf("template %s does not contain %s", tt.path, kind)
    }
   }
  })
 }
}
```

追加で `TestClusterTemplatesSetBootstrapFormat` を作り、YAML の raw text に期待 format が含まれることを確認する。

```go
tests := []struct {
 path string
 want string
}{
 {filepath.Join("..", "..", "config", "templates", "cluster-template-kubeadm-ubuntu.yaml"), "format: NoCloud"},
 {filepath.Join("..", "..", "config", "templates", "cluster-template-kubeadm-debian.yaml"), "format: Preseed"},
 {filepath.Join("..", "..", "config", "templates", "cluster-template-k3s-ubuntu.yaml"), "format: NoCloud"},
 {filepath.Join("..", "..", "config", "templates", "cluster-template-k3s-debian.yaml"), "format: Preseed"},
 {filepath.Join("..", "..", "config", "templates", "cluster-template-talos.yaml"), "format: Talos"},
}
```

- [ ] **Step 2: failing tests を確認する**

Run:

```bash
go test ./test/templates -v
```

Expected: 新規 template ファイルが存在せず失敗する。

- [ ] **Step 3: kubeadm Ubuntu template を更新する**

`config/templates/cluster-template-kubeadm.yaml` と `config/templates/cluster-template-kubeadm-ubuntu.yaml` を同じ Ubuntu NoCloud 内容にする。両方の control plane / worker `TartMachineTemplate` に以下を入れる。

```yaml
      bootstrap:
        format: NoCloud
      kernelParams:
      - console=ttyS0
      - ip=dhcp
      - autoinstall
```

`ds=nocloud-net;s=${BOOTSTRAP_METADATA_URL}` は削除する。

- [ ] **Step 4: Debian kubeadm template を追加する**

`config/templates/cluster-template-kubeadm-debian.yaml` は kubeadm Ubuntu template を元にし、kernel/initrd 変数と bootstrap format を Debian preseed にする。

```yaml
      image: ${DEBIAN_INSTALLER_KERNEL_URL}
      initrd: ${DEBIAN_INSTALLER_INITRD_URL}
      bootstrap:
        format: Preseed
      kernelParams:
      - console=ttyS0
      - ip=dhcp
      - interface=auto
```

- [ ] **Step 5: k3s templates を追加する**

`config/templates/cluster-template-k3s-ubuntu.yaml` と `config/templates/cluster-template-k3s-debian.yaml` は CAPI bootstrap/controlplane provider に依存しない infrastructure sample として、`Cluster`、`TartCluster`、control-plane 用 `TartMachineTemplate`、worker 用 `TartMachineTemplate` を含める。Ubuntu は `format: NoCloud`、Debian は `format: Preseed` にする。

`Cluster` は以下のように infrastructureRef だけを持つ。

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: ${CLUSTER_NAME}
spec:
  infrastructureRef:
    apiGroup: infrastructure.cluster.x-k8s.io
    kind: TartCluster
    name: ${CLUSTER_NAME}
```

- [ ] **Step 6: Talos template を追加する**

`config/templates/cluster-template-talos.yaml` は `TartMachineTemplate` に以下を含める。

```yaml
      image: ${TALOS_KERNEL_URL}
      initrd: ${TALOS_INITRD_URL}
      bootstrap:
        format: Talos
      kernelParams:
      - console=ttyS0
      - ip=dhcp
```

- [ ] **Step 7: samples を追加・更新する**

`config/samples/infrastructure_v1alpha1_tartmachinetemplate.yaml` は Ubuntu NoCloud sample に更新する。

```yaml
      bootstrap:
        format: NoCloud
      kernelParams:
      - console=ttyS0
      - ip=dhcp
      - autoinstall
```

`config/samples/cluster-*.yaml` は template と同じ format を具体値で持つ static sample にする。テスト用ドメインが必要な場合は `hoge.sample.walnuts.dev` を使う。

- [ ] **Step 8: kustomization と e2e config を更新する**

`config/samples/kustomization.yaml` に追加 sample を含める。

```yaml
- cluster-kubeadm-ubuntu.yaml
- cluster-kubeadm-debian.yaml
- cluster-k3s-ubuntu.yaml
- cluster-k3s-debian.yaml
- cluster-talos.yaml
```

`test/e2e/config/tart.yaml` から `BOOTSTRAP_METADATA_URL` を削除し、以下を追加する。

```yaml
  DEBIAN_INSTALLER_KERNEL_URL: "https://deb.debian.org/debian/dists/trixie/main/installer-amd64/current/images/netboot/debian-installer/amd64/linux"
  DEBIAN_INSTALLER_INITRD_URL: "https://deb.debian.org/debian/dists/trixie/main/installer-amd64/current/images/netboot/debian-installer/amd64/initrd.gz"
  TALOS_KERNEL_URL: "https://factory.talos.dev/image/installer/latest/kernel-amd64"
  TALOS_INITRD_URL: "https://factory.talos.dev/image/installer/latest/initramfs-amd64.xz"
```

- [ ] **Step 9: template tests を通す**

Run:

```bash
go test ./test/templates -v
```

Expected: PASS。

- [ ] **Step 10: コミットする**

Run:

```bash
git status --short
git --no-pager add config/templates config/samples test/templates/kubeadm_template_test.go test/e2e/config/tart.yaml
git --no-pager commit --signoff -m "multi OS bootstrapのsampleとtemplateを追加"
```

---

### Task 5: E2E の sample 受け入れ検証を拡張

**Files:**

- Modify: `test/e2e/e2e_test.go`

- [ ] **Step 1: failing E2E test を追加する**

既存の `should accept the Kubeadm TartMachineTemplate infrastructure template` を table driven に変更する。

```go
It("should accept multi OS TartMachineTemplate samples", func() {
 tests := []struct {
  name       string
  file       string
  resource   string
  wantFormat string
 }{
  {
   name:       "standalone Ubuntu NoCloud TartMachineTemplate",
   file:       "config/samples/infrastructure_v1alpha1_tartmachinetemplate.yaml",
   resource:   "tartmachinetemplate-sample",
   wantFormat: "NoCloud",
  },
  {
   name:       "kubeadm Ubuntu sample",
   file:       "config/samples/cluster-kubeadm-ubuntu.yaml",
   resource:   "tart-kubeadm-ubuntu-control-plane",
   wantFormat: "NoCloud",
  },
  {
   name:       "kubeadm Debian sample",
   file:       "config/samples/cluster-kubeadm-debian.yaml",
   resource:   "tart-kubeadm-debian-control-plane",
   wantFormat: "Preseed",
  },
  {
   name:       "k3s Ubuntu sample",
   file:       "config/samples/cluster-k3s-ubuntu.yaml",
   resource:   "tart-k3s-ubuntu-control-plane",
   wantFormat: "NoCloud",
  },
  {
   name:       "k3s Debian sample",
   file:       "config/samples/cluster-k3s-debian.yaml",
   resource:   "tart-k3s-debian-control-plane",
   wantFormat: "Preseed",
  },
  {
   name:       "Talos sample",
   file:       "config/samples/cluster-talos.yaml",
   resource:   "tart-talos-control-plane",
   wantFormat: "Talos",
  },
 }
 for _, tt := range tests {
  By("applying " + tt.name)
  cmd := exec.Command("kubectl", "apply", "-n", namespace, "-f", tt.file)
  _, err := utils.Run(cmd)
  Expect(err).NotTo(HaveOccurred(), "Failed to apply "+tt.file)

  By("validating bootstrap format for " + tt.name)
  cmd = exec.Command("kubectl", "get", "tartmachinetemplate", tt.resource,
   "-n", namespace,
   "-o", "jsonpath={.spec.template.spec.bootstrap.format}",
  )
  output, err := utils.Run(cmd)
  Expect(err).NotTo(HaveOccurred(), "Failed to get "+tt.resource)
  Expect(output).To(Equal(tt.wantFormat))
 }
})
```

- [ ] **Step 2: compile failure または dry-run failure を確認する**

Run:

```bash
go test -tags=e2e ./test/e2e -run TestE2E -ginkgo.dry-run -v
```

Expected: test は compile する。sample 名が未整備なら dry-run では検出されないため、Task 4 完了後は PASS になる。

- [ ] **Step 3: 古い E2E 検証を置き換える**

既存の `It("should accept the Kubeadm TartMachineTemplate infrastructure template", ...)` を Step 1 の table driven test に置き換える。old `jsonpath={.spec.template.spec.kernelParams[1]}` の検証は削除する。`By` の利用者向けメッセージは英語のまま維持する。

- [ ] **Step 4: E2E dry-run を通す**

Run:

```bash
go test -tags=e2e ./test/e2e -run TestE2E -ginkgo.dry-run -v
```

Expected: PASS。

- [ ] **Step 5: コミットする**

Run:

```bash
git status --short
git --no-pager add test/e2e/e2e_test.go
git --no-pager commit --signoff -m "multi OS bootstrap sampleのE2E検証を追加"
```

---

### Task 6: 全体検証と調整

**Files:**

- Verify only: `api/v1alpha1/tartmachine_types.go`
- Verify only: `internal/server/ipxe/server.go`
- Verify only: `internal/server/ipxe/server_test.go`
- Verify only: `config/templates/*.yaml`
- Verify only: `config/samples/*.yaml`
- Verify only: `test/e2e/e2e_test.go`

- [ ] **Step 1: Go unit tests を実行する**

Run:

```bash
go test ./api/v1alpha1 ./internal/server/ipxe ./test/templates -v
```

Expected: PASS。

- [ ] **Step 2: repository test task を実行する**

Run:

```bash
mise run test
```

Expected: PASS。

- [ ] **Step 3: lint を実行する**

Run:

```bash
mise run lint
```

Expected: PASS。

- [ ] **Step 4: E2E dry-run を実行する**

Run:

```bash
go test -tags=e2e ./test/e2e -run TestE2E -ginkgo.dry-run -v
```

Expected: PASS。

- [ ] **Step 6: 最終差分を確認する**

Run:

```bash
git status --short
git --no-pager diff --stat HEAD
git --no-pager log --oneline --decorate -6
```

Expected: 未コミット差分なし。コミットが設計、API、iPXE/metadata、sample/template、E2E の単位に分かれている。

- [ ] **Step 7: PR を作成する**

Run:

```bash
git --no-pager push -u origin feature/multi-os-bootstrap-metadata
gh pr create --title "multi OS bootstrap metadata配信を追加" --body "## Summary
- Add bootstrap format selection to TartMachine
- Serve Talos, NoCloud, and Preseed bootstrap metadata
- Add kubeadm/k3s Ubuntu/Debian and Talos samples

## Tests
- go test ./api/v1alpha1 ./internal/server/ipxe ./test/templates -v
- mise run test
- mise run lint
```

Expected: GitHub PR が作成される。
