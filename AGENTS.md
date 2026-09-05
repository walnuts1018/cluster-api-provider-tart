# Tart Agent Rules

このファイルは、Tartの全ての作業へ常時適用する短い規約である。設計や実装の詳細は、必要な作業に対応するskillと`docs/development/`を参照する。

## 常時適用する規約

- チャット、コードコメント、コミットメッセージは日本語で書く。ただし、log、Event、Condition、Status messageなどアプリケーション利用者に見えるメッセージは英語で書く。
- 日本語文では英単語と日本語の間に半角スペースを入れず、文の途中で改行しない。コメントは作業者との会話ではなく、コードの意図や安全上の理由を説明する。
- 一時的な実装には、理由と解消条件を含む`TODO:`を残す。`defer`した処理のerrorを握りつぶさず、必要ならlogへ出力する。
- 変更は同じ目的ごとにまとめ、コミット時は`--signoff`を使う。AI AgentをCo-Authorへ追加しない。mainへ直接コミットしてよい。
- ツールは可能な限りmiseで管理し、定期的なコマンドはmise taskとして定義する。
- Kubernetes controller、CRD、Webhookの変更にはKubebuilderまたはcontroller-genを使う。
- `docs/development/README.md`を開発の入口とし、設計を変えた場合は関連文書とskillを同じ変更で更新する。
- 何かしらの外部ツールやパッケージなどのバージョンをファイルとして記述する場合は必ず適切にrenovateで管理・アップデートできるようにしてください。複数の場所でバージョンを整合させる必要がある場合も、それをうまくrenovateで扱えるようにしてください。

## ディレクトリ構成

- ルート直下に`internal`または`pkg`という名前のディレクトリを作成しない。公開範囲ではなく、`api`、`controller`、`host`、`talos`、`bootstrap`、`controlplane`、`boot`、`extensions`などの具体的な責務で配置する。
- Webアプリケーション由来の`domain`、`infrastructure`、`workflow`のような大分類を、複数の責務を隠すために新設しない。
- interfaceは、外部副作用を隔離する、または実際に複数の実装が存在する場合だけ定義する。将来の可能性だけで抽象化しない。

## Tartの範囲

- TartはTalos Linux専用のInfrastructure Provider、Bootstrap Provider、Control Plane Providerである。Kubeadm、Ubuntu、汎用OS provisioning framework、既存Talos Providerへ対応するための互換層は作らない。
- Talosのinstaller、machine configuration、disk/volume、upgrade、rollback、etcd bootstrap、Kubernetes runtimeを再実装しない。
- `TartHost`はCAPI Machineより長寿命のinventory、`TartMachine`はCAPI Machineとのbindingを表す。通常のupdateでHost claim、Machine、diskを破棄しない。
- `TartHost`はmanagement cluster全体で一意なcluster-scoped inventoryとし、MAC address、system UUIDなどのstable identityをHost間で重複させない。Claim中のHostは削除をBlockし、Retained Hostのforgetは明示的なannotationを必要とする。forgetはpower off、reset、disk wipeを行わない。
- `TartHostOperation`、Operation CRD、Workflow engine、Provisioning Plan、独自Provisioning Agent、独自Node Lifecycle Agent、独自OS image format、独自disk writer、add-on専用APIを作らない。
- Provider APIは`v1alpha1`へリセットし、Infrastructure、Bootstrap、Control Planeをそれぞれ`infrastructure.cluster.x-k8s.io`、`bootstrap.cluster.x-k8s.io`、`controlplane.cluster.x-k8s.io`へ分ける。CAPI coreの現行v1beta2 contractとは別のversionである。

## 安全性

- Resource Statusは観測結果とConditionsだけを保持し、workflowのprogram counterやstep番号として利用しない。
- controller再起動後もKubernetes desired state、TartHost observed state、Talos API、必要なworkload clusterの観測からreconcileを継続できるようにする。
- in-place updateできない変更をMachine replacementへ暗黙にfallbackしない。identity変更、destructive storage change、quorumを守れない操作はblockedとして報告する。
- Runtime Extensionの`CanUpdateMachineSet`、`CanUpdateMachine`はdesired diff全体をcoverできる安全な差分だけを`Success`と完全なpatchで返し、unsafe、unknown、partial diffはpatchなしの`Failure`で止める。初回provisioning後のmutableなTalos OS/config変更を実行できるのはUpdate Extensionだけで、通常のInfrastructure/Bootstrap reconcileは観測とStatus反映だけを行う。
- Control Plane Providerがin-place updateを開始するときは`CanUpdateMachine`成功後にCAPI Machine、InfraMachine、BootstrapConfigをannotation付きで更新し、Machineへ`UpdateMachine` hook pendingを設定する。この遷移はrace-free、re-entrantに観測から再開できるようにする。
- `TartHost.spec.consumerRef`をallocation bindingの正本とし、`status.claimedBy`をlockに使わない。Machine削除時はcontroller-managedな`spec.retainedFrom`へ直前のconsumer UIDを残し、Hostを`Retained`として保持する。現在の`retainedFrom`に一致する明示的な再利用承認と`Adopt`/`Reprovision` modeがそろうまで自動allocationしない。
- ProviderIDはHost allocation後にHost UIDから`tart://host/<TartHost UID>`として決定する。Infrastructure Providerはbootstrap dataを待たずにallocation bindingとProviderIDを確立できるが、Talos provisioningはbootstrap dataを待つ。Infrastructure ProviderとBootstrap Providerは同じ決定論的な生成規則を使い、Talos kubeletとNode `spec.providerID`を一致させる。
- Machine deletionのdrainとvolume detachはCAPI Machine controller、scale-down時のetcd member removalはControl Plane Providerのpre-terminate delete hook、Talos shutdownと停止確認・retention・claim処理はTartMachine finalizerが担当する。停止を確認できない場合はclaimを保持してBlockする。Cluster全体の削除ではetcd quorum維持を必須にしない。
- `Reusable`はwipeの同義語にしない。`Adopt`は既存identityとconfigurationが一致する場合だけdataを保持し、`Reprovision`はdata破棄を明示承認した別lifecycleとしてTalos reset/installerへ委譲する。通常updateやdeleteのfallbackから到達させない。
- Cluster secret bundleはControl Plane Providerが一度だけ作成し、全Managed Machineのshutdownとretention完了後までGCしない。Cluster削除後にbundleが失われたRetained Hostは`Adopt`不可、`Reprovision`専用とし、自動wipeせずmaintenance boot capabilityがなければBlockする。
- Tart-managed MachineはすべてMHCのdelete-and-recreate remediationを安全な既定値とみなさず、初期運用では`cluster.x-k8s.io/skip-remediation`を使う。明示的なreplacement opt-inなしにMachineを削除しない。
- Secret、credential、private key、Bootstrap Data、kubeconfigをStatus、Event、通常log、metrics labelへ出力しない。

## 作業開始時の参照先

- 全体設計: `.agents/skills/architecture/SKILL.md`と`docs/development/README.md`
- CAPI contract: `.agents/skills/cluster-api/SKILL.md`
- Reconcile: `.agents/skills/reconcile/SKILL.md`
- Talos連携: `.agents/skills/talos/SKILL.md`
- Host lifecycle: `.agents/skills/host-lifecycle/SKILL.md`
- 検証: `docs/development/verification.md`

## 現在の検証方針

新設計を組み立てる間は、新しいGo testを追加せず、Go testも実行しない。生成、format、build、vet、lint、manifest検証など、テスト以外の静的検証を行い、解除時には`docs/development/verification.md`と`gotest` skillを先に更新する。
