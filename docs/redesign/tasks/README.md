# タスク一覧

| # | タスク | 主な成果 | 依存 |
|---|---|---|---|
| 01 | [基礎検証とADR確定](01-foundation-spikes.md) | CAPI hook、A/B、RO root、rollbackの実証 | なし |
| 02 | [APIとドメイン再設計](02-api-and-domain.md) | CRD、conversion、状態機械、排他割当 | 01 |
| 03 | [Driver抽象化とWoL移行](03-driver-abstraction.md) | capability port、registry、WoL adapter | 01 |
| 04 | [Agent protocolと安全な配信](04-agent-protocol.md) | versioned API、session、bundle、再開 | 02、05のmanifest契約 |
| 05 | [OS成果物ビルド](05-image-pipeline.md) | slot image、manifest、署名、SBOM | 01 |
| 06 | [Agentとネットワークブート統合](06-network-boot-agent.md) | 一時OS起動、agent実装、inventory | 03、04、05 |
| 07 | [初期プロビジョニング](07-initial-provisioning.md) | Ubuntu 24.04 + kubeadm縦スライス | 02、06 |
| 08 | [OS-only A/Bインプレース更新](08-ab-update.md) | Runtime Hooks、health gate、slot rollback | 07 |
| 09 | [Kubernetes Distribution Lifecycle](09-kubernetes-lifecycle.md) | kubeadm upgrade、snapshot、health | 08 |
| 10 | [Redfish対応](10-redfish.md) | power、boot transport、Virtual Media | 03、06 |
| 11 | [対応拡大とリリース](11-compatibility-and-release.md) | k3s、各OS/boot/arch、runbook | 09、10 |

## GitHub Issue運用

- この一覧自体を実装状態の正本にしない。実装開始時に各タスクをGitHub Issueとして作成する。
- Issue #143、#145、#146、#147を親または関連Issueとしてリンクする。
- 1タスクが1 PRを超える場合は、受け入れ条件単位のsub-issueへ分ける。
- PRには対象Issueを`Closes #...`で関連付け、設計変更があればADRを同じPRで更新する。
- 各タスクは専用ブランチで実装し、署名付きの小さなコミットへ分割する。

## 共通Definition of Done

- 受け入れ条件が自動テストまたは実機検証記録で確認できる。
- Reconcileと外部操作の冪等性、timeout、retry上限、途中再開が確認できる。
- Condition、Event、ログは英語で、Secretを含まない。
- Tracer/Meterはプロジェクトのglobal providerを使用する。
- CRD/Webhook変更はKubebuilder/controller-genで生成する。
- application logic以外のファイル存在確認テストを追加しない。
- `mise run test-e2e`と`mise run test-provisioning-e2e`はローカル実行せず、GitHub Actionsで実行する。
