# Agent Artifact配信 実装計画

> **実装時の必須スキル:** `superpowers:executing-plans`を使用し、各項目をテスト先行で実装する。

**目的:** `amd64-uefi-ab/v1`用Agent Artifactを署名検証後だけ配信し、active Operationを持つHostへ起動用iPXE scriptを返す。

**構成:** 署名対象manifestと検証を`dto/agent_artifact`へ置き、HTTPやKubernetesから独立した純粋ロジックにする。Kubernetes AdapterはMACアドレスからHostとactive Operationを解決し、HTTP Serverは検証済みArtifactの固定digest URL配信とiPXE script生成だけを担当する。

**技術:** Go、Ed25519、RFC 8785 Canonical JSON、controller-runtime client、Echo

---

### Task 1: Agent Artifact manifestと署名検証

**Files:**

- Create: `dto/agent_artifact/manifest.go`
- Create: `dto/agent_artifact/verify.go`
- Test: `dto/agent_artifact/manifest_test.go`

- [x] manifestの正規化、digest固定OCI参照、amd64/UEFI/Profile、kernel/initrd descriptorを検証する失敗テストを書く。
- [x] `MISE_OFFLINE=1 mise exec -- go test ./dto/agent_artifact -v`で未実装による失敗を確認する。
- [x] RFC 8785 canonical JSONとEd25519署名検証、payloadのsize/digest検証を実装する。
- [x] 同じテストを再実行して成功を確認する。
- [x] `feat: Agent Artifactの署名検証を追加する`としてコミットする。

### Task 2: 起動対象解決とiPXE script生成

**Files:**

- Create: `domain/shared/agentboot/script.go`
- Create: `domain/shared/agentboot/script_test.go`
- Create: `infrastructure/repository/k8s/agentboot/resolver.go`
- Create: `infrastructure/repository/k8s/agentboot/resolver_test.go`

- [x] URLと識別子のescaping、秘密値非包含、対象Host/Profile/Operation phaseを検証する失敗テストを書く。
- [x] 対象packageの`go test`で期待した失敗を確認する。
- [x] 純粋なscript builderとv1beta1 Host/Operation resolverを実装する。
- [x] 対象packageの`go test`を再実行する。
- [x] `feat: Agent起動対象とiPXEスクリプトを生成する`としてコミットする。

### Task 3: Artifact HTTP配信とcontroller wiring

**Files:**

- Create: `infrastructure/http_server/agentboot/handler.go`
- Create: `infrastructure/http_server/agentboot/handler_test.go`
- Modify: `cmd/controller-manager/main.go`
- Modify: `config/manager/manager.yaml`

- [x] kernel/initrdのdigest固定URLだけを配信し、未検証Artifactをserverへ渡せないことを示す失敗テストを書く。
- [x] handler testの失敗を確認する。
- [x] 起動時検証、HTTP route、設定flag、leader election対象serverへの接続を実装する。
- [x] handler、cmd、関連server testを実行する。
- [x] `feat: Agent Artifact配信をcontrollerへ接続する`としてコミットする。

### Task 4: 文書更新と検証

**Files:**

- Modify: `docs/redesign/tasks/06-network-boot-agent.md`
- Modify: `docs/redesign/platform-profiles/amd64-uefi-ab-v1.md`

- [x] 実装済み範囲、設定、残作業を更新する。
- [ ] `MISE_OFFLINE=1 mise exec -- go test ./...`、`MISE_OFFLINE=1 mise run build`、`MISE_OFFLINE=1 mise run lint`、`git diff --check`を実行する。
- [ ] E2Eはローカル実行せず、自己レビューでCritical/Importantを解消する。
- [ ] PRへTask 6、受け入れ条件1/2の既存証跡、今回の検証commandを記載して作成し、既定方針どおりマージする。
