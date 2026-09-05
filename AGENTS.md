# Tart Agent Rules

このファイルは、Tartの全ての作業へ常時適用する短い規約である。設計や実装の詳細は、必要な作業に対応するskillと`docs/development/`を参照する。

## 常時適用する規約

- チャット、コードコメント、コミットメッセージは日本語で書く。ただし、log、Event、Condition、Status messageなどアプリケーション利用者に見えるメッセージは英語で書く。
- 日本語文では英単語と日本語の間に半角スペースを入れず、文の途中で改行しない。コメントは作業者との会話ではなく、コードの意図や安全上の理由を説明する。
- 一時的な実装には、理由と解消条件を含む`TODO:`を残す。`defer`した処理のerrorを握りつぶさず、必要ならlogへ出力する。
- 変更は同じ目的ごとにまとめ、コミット時は`--signoff`を使う。AI AgentをCo-Authorへ追加しない。mainへ直接コミットしてよい。
- ツールは可能な限りmiseで管理し、定期的なコマンドはmise taskとして定義する。
- Kubernetes controller、CRD、Webhookの変更にはKubebuilderまたはcontroller-genを使う。
- `docs/development/README.md`を開発の入口とし、設計を変えた場合は関連文書とskillを同じ変更で更新する。未実装機能のタスクは`docs/development/tasks.md`で管理する。
- 何かしらの外部ツールやパッケージなどのバージョンをファイルとして記述する場合は必ず適切にrenovateで管理・アップデートできるようにしてください。複数の場所でバージョンを整合させる必要がある場合も、それをうまくrenovateで扱えるようにしてください。
- 並列に複数のCoding Agentが作業することがあります。他のエージェントの作業を上書きしないように、他のエージェントが作業している箇所を編集しないようにしてください。
- ダミーやテスト用のドメインには`test.walnuts.dev`や`sample.walnuts.dev`を使う。また、テストなどでのMACアドレスは00-00-5E-00-53-00から00-00-5E-00-53-FFの範囲を使う。IPアドレスはRFC 5737で予約された範囲を使う。
- sub-agentを積極的に活用してください。ただし、毎回親と同じモデルを使うのではなく、基本的にはgpt-5.6-luna (high, extra high, max)やSonnet 5 low などを活用し、なるべく低コストで済むようにしてください。重たいモデルで複数のSubAgentを使うのはなるべく避けてください。
  - When using subagents:
    - Do not repeatedly poll or wait for subagents.
    - While subagents are running, continue any useful non-overlapping work.
    - Call wait_agent only when the main task is genuinely blocked on a subagent result.
    - Prefer the longest practical wait timeout rather than frequent short polling.
    - If a wait times out and the agent is still running, do not immediately enter a repeated wait loop unless there is no other productive work available.
    - Collect and integrate completed subagent results in batches where possible.

## ディレクトリ構成

- ルート直下に`internal`または`pkg`という名前のディレクトリを作成しない。
- interfaceは、外部副作用を隔離する、または実際に複数の実装が存在する場合だけ定義する。将来の可能性だけで抽象化しない。

## Tartの範囲

- TartはTalos Linux専用のInfrastructure Provider、Bootstrap Provider、Control Plane Providerである。Kubeadm、Ubuntu、汎用OS provisioning framework、既存Talos Providerへ対応するための互換層は作らない。
- Talosのinstaller、machine configuration、disk/volume、upgrade、rollback、etcd bootstrap、Kubernetes runtimeを再実装しない。
- `TartHost`はCAPI Machineより長寿命のinventory、`TartMachine`はCAPI Machineとのbindingを表す。通常のupdateでHost claim、Machine、diskを破棄しない。
- `TartHostOperation`、Operation CRD、Workflow engine、Provisioning Plan、独自Provisioning Agent、独自Node Lifecycle Agent、独自OS image format、独自disk writer、add-on専用APIを作らない。
- Provider APIは`v1alpha1`へリセットし、Infrastructure、Bootstrap、Control Planeをそれぞれ`infrastructure.cluster.x-k8s.io`、`bootstrap.cluster.x-k8s.io`、`controlplane.cluster.x-k8s.io`へ分ける。CAPI coreの現行v1beta2 contractとは別のversionである。

## 安全性

- Resource Statusは観測結果とConditionsだけを保持し、workflowのprogram counterやstep番号として利用しない。
- controller再起動後もKubernetes desired state、TartHost observed state、Talos API、必要なworkload clusterの観測からreconcileを継続できるようにする。
- in-place updateできない変更をMachine replacementへ暗黙にfallbackせず、`Ready=False`で安全停止する。
- Runtime Extensionの`CanUpdateMachineSet`、`CanUpdateMachine`はdesired diff全体をcoverできる安全な差分だけを`Success`と完全なpatchで返し、unsafe、unknown、partial diffはpatchなしの`Failure`で止める。
- `TartHost.spec.consumerRef`をallocation bindingの正本とし、`status.claimedBy`をlockに使わない。claimはSSAではなくatomic CASで確立する。
- Machine削除後は`spec.retainedFrom`を記録し、Hostを`Retained`として保持する。明示的な承認なしに自動allocationしない。
- ProviderIDはHost allocation後に`TartHost.spec.id`から`tart://host/<TartHost.spec.id>`として決定論的に生成し、Talos kubeletとNode `spec.providerID`を一致させる。
- Machine deletionでは、CAPI Machine controllerのdrain、Control Planeのetcd脱退hook、TartMachine finalizerのshutdownと停止確認・retention処理を責務どおりに分離する。停止未確認時はclaimを解除しない。
- ユーザーのraw Talos configuration patchは全てimmutableなSecret-backed inputへ格納し、CRD Specへinline保存する経路を作らない。
- Tart-managed MachineはすべてMHCのdelete-and-recreate remediationを抑止するため、生成前から`cluster.x-k8s.io/skip-remediation`を設定する。
- Secret、credential、private key、Bootstrap Data、kubeconfigをStatus、Event、通常log、metrics labelへ出力しない。

## 作業開始時の参照先

- 全体設計: `.agents/skills/architecture/SKILL.md`と`docs/development/README.md`
- CAPI contract: `.agents/skills/cluster-api/SKILL.md`と`docs/development/api-contract.md`
- Reconcile: `.agents/skills/reconcile/SKILL.md`
- Talos連携: `.agents/skills/talos/SKILL.md`と`docs/development/talos.md`
- Host lifecycle: `.agents/skills/host-lifecycle/SKILL.md`と`docs/development/lifecycle.md`
- 未実装タスク: `docs/development/tasks.md`
- 検証: `docs/development/verification.md`

## 現在の検証方針

Go testを全面禁止しない。実装と同時に、Host claim race、Retained gate、unsafe diffのfail-closed判定、reuse approvalの世代不一致、quorum判定、configuration invariant conflict、semantic digest、外部contract、controller再起動後の再計算など、失敗時の影響が大きい境界へ最小限のtable test、fuzz test、契約テストを追加する。設定ファイルの存在確認やmock呼出し順だけのテストは追加しない。生成、format、build、vet、lint、manifest検証などの静的検証と、実機依存のTalos、storage、reboot、rollback、drain、CAPI minorごとのreplacement不発を検証するE2Eは責務を分けて実行する。
