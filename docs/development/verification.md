# 検証方針

この文書は、Tart Providerの実装を検証するための静的検証手順、単体テスト方針、および受け入れ確認項目を定義する。

---

## 静的検証

コード変更時は、以下の静的検証コマンドを実行して整合性を確認する。

| 対象 | コマンド | 確認内容 |
| --- | --- | --- |
| **フォーマット** | `mise run fmt` | コードのフォーマット（`gofmt`） |
| **コード生成** | `mise run generate` | DeepCopy等のコード自動生成 |
| **マニフェスト** | `mise run manifests` | CRDおよびRBACマニフェストの生成・更新 |
| **ビルド** | `mise run build` | 全パッケージのコンパイル確認 |
| **静的解析** | `mise run lint:fix` / `go vet ./...` | golangci-lint等による静的解析 |
| **単体テスト** | `mise run test` | 単体テストの実行 |

生成物の検証に必要なツール（`controller-gen`、`kustomize`等）は、必ず `mise` で管理されたバージョンを使用すること。

---

## テストの境界と方針

Go test（単体テスト）は、外部依存を持たずにテスト可能な**純粋な計算・判断・不変条件の検証**に集中させる。

### 単体テストで検証する境界
- Host claimの競合判定（atomic CAS）およびEligibility分類（[`host/`](../../host)）
- effective configurationのSHA-256 digest算出および秘匿化（[`bootstrap/`](../../bootstrap)）
- etcd quorum計算および安全なレプリカ数判定（[`controlplane/`](../../controlplane)）
- 不変条件の競合検知（Provider-owned fieldとuser patchの不整合）
- Bootstrap SecretのCAPI契約整合性検証
- Redfish Service Root、System選択、Reset action、TLS/credential検証および`PowerState=Off`停止確認のHTTP契約

### E2E / 実環境で検証する境界
モックの呼び出し順だけを検証する脆い単体テストは作成せず、以下のような実機・実API依存の処理はE2Eテスト（またはenvtest/統合環境）で検証する。
- 実際のmaintenance Talos APIへの接続とハードウェアインベントリ取得
- Talos OSインストールおよびHost再起動後の認証済み相互TLS API復帰
- 実環境でのin-place update、drain、およびロールバック検知
- 削除時の実際のTalos shutdownと停止確認
- 実機BMCのvendorごとのRedfish互換性、GracefulShutdown後の電源断およびcredential/CA rotation

---

## 実装後の受け入れ確認項目

各機能が完成した際の受け入れ条件は以下のとおりである（未実装タスクの進捗は[実装タスク一覧](tasks.md)を参照）。

1. **Fresh Machine**:
   - 最小限のHost登録から、Bootstrap SecretなしでDiscovery bootが行われ、ハードウェアインベントリが観測されること。
   - Bootstrap Secret取得後にTalos OSインストールが進行し、認証済みAPI到達とInfrastructureReadyが確認されること。
2. **Cluster Lifecycle**:
   - 単一ノードControl PlaneおよびHA Control Planeが正常に作成され、quorumを維持して動作すること。
   - Worker Machineが作成され、同一Host上でin-place updateできること。
3. **安全停止とデータ保護**:
   - 安全にin-place updateできない変更に対して、Machine replacementへ暗黙にフォールバックせず `Ready=False` で安全停止すること。
   - Machine削除時にTalos shutdownと停止確認が行われ、Hostが `Retained` として保護されること。
   - `allowDowntime: false`（既定）において、drain失敗時に更新が中断されること。
4. **Recovery（自己修復・再開性）**:
   - provisioning、reboot、upgrade、bootstrap API呼び出し直後にcontroller-managerを停止・再起動しても、Resourceの手動修復なしに外部観測状態からreconcileが継続すること。
   - 呼び出し直後に停止しても、完了済みoperationを二重に再初期化しないこと。
5. **証跡の同一性と機密情報保護**:
   - Status、Event、通常ログ、メトリクスラベルに、Secretデータ、private key、認証トークンを出力・保存しないこと。
   - 更新前後のノード同一性判定は、Pod名やDHCPアドレスではなく、`TartHost.spec.id`、CAPI Machine UID、TartMachine UID、安定したdisk identityで行うこと。
   - 永続volume上のsentinel payload（テスト用データ）が、Talos minor update、schematic変更、設定更新の前後で維持されていることを検証すること（disk identityの一致だけでデータ保持の証拠としない）。
