# Task 07 初期Provisioning 実装計画

> **実装者向け:** 各作業単位をTDDで進め、完了した項目をチェックする。

**目的:** CAPI MachineとCABPK Bootstrap Secretから、再起動可能なProvision Operationを開始し、OS boot・Bootstrap適用・Node healthの全条件成立後だけTartMachineをProvisionedにする。

**構成:** TartMachine controllerはKubernetes objectの取得とPatchだけを担当する。Host選択、Operation入力生成、完了判定は副作用のないdomain/applicationロジックへ分離し、Kubernetes adapterが予約・Operation・Plan Secret・Host phaseを永続化する。

**技術:** Go、controller-runtime、Cluster API v1beta2、RFC 8785 Canonical JSON、Ed25519、envtest。

---

### 作業単位1: 途中実装の状態遷移と冪等性を修復する

**対象:**

- `internal/application/initialprovisioning/orchestrator.go`
- `internal/application/initialprovisioning/orchestrator_test.go`
- `internal/application/initialprovisioning/readiness.go`
- `internal/application/initialprovisioning/readiness_test.go`
- `internal/controller/tartmachine_v1beta1_controller.go`
- `internal/controller/tartmachine_v1beta1_controller_test.go`
- `internal/adapter/k8s/allocation/service.go`
- `internal/adapter/k8s/allocation/service_test.go`

- [x] 予約済みHostを同じMachineが再取得できる失敗テストを追加する。
- [x] OperationRefのStatus Patch前にcontrollerが停止しても既存予約からOperation作成を再開するテストを追加する。
- [x] `AwaitingHealth`、boot reportのState/Data mount、Bootstrap marker、Node Ready、providerID一致の全条件が揃わない限りProvisionedにならないテストを追加する。
- [x] OperationのdeadlineとIDが同じ入力の再実行で変化しない構成へ修正する。
- [x] 対象packageのテストを実行し、変更をコミットする。

### 作業単位2: 署名済みProvision Planを生成・保存する

**対象:**

- `internal/application/initialprovisioning/plan.go`
- `internal/application/initialprovisioning/plan_test.go`
- `internal/adapter/k8s/agentapi/plan_writer.go`
- `internal/adapter/k8s/agentapi/plan_writer_test.go`
- `cmd/main.go`
- `cmd/wire/wire.go`
- `cmd/wire/wire_gen.go`

- [x] TartHost root device、TartMachine image、Operation deadline、Bootstrap formatから`agentprotocol.Plan`を構築する失敗テストを追加する。
- [x] Planを検証してRFC 8785 digestを算出し、Ed25519署名する。
- [x] Operationと同じ名前空間へowner reference付きSecretをSSAで保存する。
- [x] controller再起動後に同じPlan Secretへ収束するテストを追加する。
- [x] Plan署名鍵をArtifact信頼鍵とは別のread-only mountから読み込む起動オプションを追加する。
- [x] 対象packageのテストを実行し、変更をコミットする。

### 作業単位3: Bootstrap Bundleと初回boot完了を接続する

**対象:**

- `internal/adapter/k8s/agentapi/provider.go`
- `internal/adapter/k8s/agentapi/provider_test.go`
- `internal/adapter/k8s/bootreport/service.go`
- `internal/adapter/k8s/bootreport/service_test.go`
- `internal/application/initialprovisioning/readiness.go`

- [x] CABPK Secretの`format=cloud-config`とpayload digestを検証する。
- [x] Bundle送信成功時にSession Token Secretを即時削除する既存処理をOperation作成フローと結合する。
- [x] Bootstrap成功markerがpayload digestと一致するProtocolへ拡張する。
- [x] boot report受信後もNode health条件が不足していれば`AwaitingHealth`を維持する。
- [x] 全条件成立時だけOperation、Host、TartMachineを順に完了状態へPatchする。
- [x] 対象packageのテストを実行し、変更をコミットする。

### 作業単位4: 削除Policyと手動WipeAllを実装する

**対象:**

- `internal/application/cleaning/`
- `internal/controller/tartmachine_v1beta1_controller.go`
- `internal/controller/tarthostoperation_controller.go`
- `internal/adapter/k8s/v1beta1host/service.go`
- `cmd/wire/wire.go`
- `cmd/wire/wire_gen.go`

- [x] `WipeAll`、`RetainData`、`RetainState`をPlanの許可Disk Roleへ写像するtable testを追加する。
- [x] disk容量からWipeAll deadlineを算出する純粋関数と境界値テストを追加する。
- [x] TartMachine削除時にCleaning Operationを作成し、完了までfinalizerを保持する。
- [x] `machineRef=nil`の手動WipeAllを同じOperation reconcilerで処理する。
- [x] 完了後のHost phaseをそれぞれAvailable、Retained、DetachedへPatchする。
- [x] 対象packageのテストを実行し、変更をコミットする。

### 作業単位5: 検証記録と統合

**対象:**

- `docs/redesign/tasks/07-initial-provisioning.md`

- [x] 受け入れ条件ごとの実装状況と残る実機確認を記録する。
- [ ] `mise run build`、`mise run lint`、関連する`go test`を実行する。
- [ ] `mise run test-provisioning-e2e`はローカル実行せず、GitHub Actionsの結果だけを記録する。
- [ ] PR本文に対象条件、検証command、未検証の実機項目を記載する。
