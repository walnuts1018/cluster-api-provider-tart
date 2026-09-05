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
- 並列に複数のCoding Agentが作業することがあります。他のエージェントの作業を上書きしないように、他のエージェントが作業している箇所を編集しないようにしてください。
- ダミーやテスト用のドメインには`test.walnuts.dev`や`sample.walnuts.dev`を使う。また、テストなどでのMACアドレスは00-00-5E-00-53-00空00-00-5E-00-53-FFの範囲を使う。IPアドレスはRFC 5737で予約された範囲を使う。

## ディレクトリ構成

- ルート直下に`internal`または`pkg`という名前のディレクトリを作成しない。公開範囲ではなく、`api`、`controller`、`host`、`talos`、`bootstrap`、`controlplane`、`boot`、`extensions`などの具体的な責務で配置する。
- Webアプリケーション由来の`domain`、`infrastructure`、`workflow`のような大分類を、複数の責務を隠すために新設しない。
- interfaceは、外部副作用を隔離する、または実際に複数の実装が存在する場合だけ定義する。将来の可能性だけで抽象化しない。

## Tartの範囲

- TartはTalos Linux専用のInfrastructure Provider、Bootstrap Provider、Control Plane Providerである。Kubeadm、Ubuntu、汎用OS provisioning framework、既存Talos Providerへ対応するための互換層は作らない。
- Talosのinstaller、machine configuration、disk/volume、upgrade、rollback、etcd bootstrap、Kubernetes runtimeを再実装しない。
- `TartHost`はCAPI Machineより長寿命のinventory、`TartMachine`はCAPI Machineとのbindingを表す。通常のupdateでHost claim、Machine、diskを破棄しない。
- `TartHost`はmanagement cluster全体で一意なcluster-scoped inventoryとし、Kubernetes metadata UIDから独立したimmutableな`spec.id`を持つ。MAC address、system UUIDなどのstable identityの重複を観測した場合は関係するHostを`Ready=False`、`Reason=IdentityConflict`としてallocationとmaintenance configuration applyを停止する。Claim中のHostは削除をBlockし、Retained Hostのforgetは明示的なannotationを必要とする。forgetはpower off、reset、disk wipeを行わない。
- `TartHost.spec.id`と`TartCluster.spec.id`はTemplateやSSA dry-runのdefaultingで生成しない。concrete Resourceのnon-dry-run CREATE後にprovider controllerが一度だけ生成して永続化し、DR復元だけバックアップ済みのIDを保持する。ID確定前にbundle生成、Host claim、provisioningを開始しない。同名Clusterの再作成では新しいCluster IDを使う。
- `TartHostOperation`、Operation CRD、Workflow engine、Provisioning Plan、独自Provisioning Agent、独自Node Lifecycle Agent、独自OS image format、独自disk writer、add-on専用APIを作らない。
- Provider APIは`v1alpha1`へリセットし、Infrastructure、Bootstrap、Control Planeをそれぞれ`infrastructure.cluster.x-k8s.io`、`bootstrap.cluster.x-k8s.io`、`controlplane.cluster.x-k8s.io`へ分ける。CAPI coreの現行v1beta2 contractとは別のversionである。

## 安全性

- Resource Statusは観測結果とConditionsだけを保持し、workflowのprogram counterやstep番号として利用しない。
- controller再起動後もKubernetes desired state、TartHost observed state、Talos API、必要なworkload clusterの観測からreconcileを継続できるようにする。
- in-place updateできない変更をMachine replacementへ暗黙にfallbackしない。identity変更、destructive storage change、quorumを守れない操作はCAPI-facing Resourceの`Ready=False`または`Available=False`と具体的なreasonで報告する。CAPI minorごとにunsafe diffでMachineSet、Machine、TartHost claimが一つも作成されないことをE2Eで確認する。
- Runtime Extensionの`CanUpdateMachineSet`、`CanUpdateMachine`はdesired diff全体をcoverできる安全な差分だけを`Success`と完全なpatchで返し、unsafe、unknown、partial diffはpatchなしの`Failure`で止める。初回provisioning後のmutableなTalos OS/config変更を実行できるのはUpdate Extensionだけで、通常のInfrastructure/Bootstrap reconcileは観測とStatus反映だけを行う。
- Control Plane Providerがin-place updateを開始するときは`CanUpdateMachine`成功後にCAPI Machine、InfraMachine、BootstrapConfigをannotation付きで更新し、Machineへ`UpdateMachine` hook pendingを設定する。この遷移はrace-free、re-entrantに観測から再開できるようにする。
- `TartHost.spec.consumerRef`をallocation bindingの正本とし、`status.claimedBy`をlockに使わない。Machine削除時はcontroller-managedな`spec.retainedFrom`へ直前のconsumer UIDを残し、Hostを`Retained`として保持する。現在の`retainedFrom`に一致する明示的な再利用承認と`Adopt`/`Reprovision` modeがそろうまで自動allocationしない。
- ProviderIDはHost allocation後に`TartHost.spec.id`から`tart://host/<TartHost.spec.id>`として決定する。Infrastructure ProviderとDiscovery bootはbootstrap dataを待たずにallocation binding、ProviderID、inventoryを確立できるが、Talos provisioningはbootstrap dataを待つ。Infrastructure ProviderとBootstrap Providerは同じ決定論的な生成規則を使い、Talos kubeletとNode `spec.providerID`を一致させる。management clusterの復元でmetadata UIDが変わってもProviderIDを変えない。
- Machine deletionのdrainとvolume detachはCAPI Machine controller、scale-down時のetcd member removalはControl Plane Providerのpre-terminate delete hook、Talos shutdownと停止確認・retention・claim処理はTartMachine finalizerが担当する。停止を確認できない場合はclaimを保持して`Ready=False`、`Reason=ShutdownUnconfirmed`にする。node-disruptiveなupdateでは、まずclusterのavailability requirementに応じてTalosの安全なdrainまたはworkload cluster側のcordon/drainを試す。drain失敗がPDB、capacity、availabilityなどavailabilityだけの理由で、`TartCluster.spec.updatePolicy.allowDowntime: true`が明示されている場合はgraceful shutdown/rebootを許可する。未指定または`false`ならavailability理由でも安全停止する。destructive disk change、identity mismatch、Host mismatch、unsafe etcd membership change、quorum violationは`allowDowntime`で緩和しない。Cluster全体の削除ではetcd quorum維持を必須にしない。
- `Reusable`はwipeの同義語にしない。`Adopt`は既存identityとconfigurationが一致する場合だけdataを保持し、`Reprovision`はdata破棄を明示承認した別lifecycleとしてTalos reset/installerへ委譲する。通常updateやdeleteのfallbackから到達させない。
- `TartCluster.spec.id`をCAPI `Cluster.metadata.uid`から独立したimmutableなworkload cluster identityとする。`retainedFrom.clusterID`、secret bundle、Adopt、DRの関連付けはこのIDで検証し、同名Clusterの再作成で古いdataを再利用しない。
- ユーザーのraw Talos configuration patchは全てimmutableなSecret-backed inputへ格納し、CRD Specへinline保存する経路を作らない。Secretには非機密configurationを含めてもよい。
- Cluster secret bundleはControl Plane Providerがgeneration単位でimmutableに作成し、active generationを永続参照から切り替えられるようにする。CA rotationではgeneration Nを基にrotation対象のTalos/Kubernetes CAだけを更新した完全なgeneration N+1 bundleを作成し、`Pending` Secretとして先に永続化する。その後、Talosが公式に定義するaccepted CA追加、issuing CA切替、certificate refresh、旧CA削除のsemanticsをTalos machine configuration/APIでreconcileする。controllerは自動`rotate-ca`をブラックボックスとして呼び出して完了後にmaterialを回収する方式を採用せず、Pending bundleとTalosのobserved accepted/issuing CAから再開できるようにする。正常完了を観測してから新generationをactiveに確定する。Cluster存続中は過去generationをGCせず、削除時にDR保持方針を確認した後だけGCを許可する。Cluster削除後にbundleが失われたRetained Hostは`Adopt`不可、`Reprovision`専用とし、自動wipeせずmaintenance boot capabilityがなければ`Ready=False`、`Reason=SecretBundleUnavailable`にする。
- `CanUpdateMachineSet`と`CanUpdateMachine`はSecret参照名ではなく、old/new双方のimmutable Secretを解決してrenderしたeffective Talos configurationのsemantic diff全体を判定する。missing、unreadable、generation不明は`unknown`としてpatchなしの`Failure`にする。Statusへ公開するconfiguration digestはsecret-bearing valueをredactしたcanonical semantic representationのSHA-256とし、更新安全性の正本には使わない。
- Tart-managed MachineはすべてMHCのdelete-and-recreate remediationを安全な既定値とみなさず、MachineSetまたはControl PlaneのMachine templateへ生成前から`cluster.x-k8s.io/skip-remediation`を設定する。Machine作成後の後追いannotationだけに依存しない。Tart v1alpha1では自動replacementや同じlogical Machineへのguided reprovisionのopt-inを提供しない。利用者がMachineを明示的に削除するとCAPIの通常replacement semanticsが発生し得るが、replacementが別のAvailable Hostをclaimする可能性がある。Retained Hostの`Reprovision`承認はそのHostのdata破棄を許可するだけで、Machine削除や同じHostへの再割り当てを自動開始しない。
- Secret、credential、private key、Bootstrap Data、kubeconfigをStatus、Event、通常log、metrics labelへ出力しない。

## 作業開始時の参照先

- 全体設計: `.agents/skills/architecture/SKILL.md`と`docs/development/README.md`
- CAPI contract: `.agents/skills/cluster-api/SKILL.md`
- Reconcile: `.agents/skills/reconcile/SKILL.md`
- Talos連携: `.agents/skills/talos/SKILL.md`
- Host lifecycle: `.agents/skills/host-lifecycle/SKILL.md`
- 検証: `docs/development/verification.md`

## 現在の検証方針

Go testを全面禁止しない。実装と同時に、Host claim race、Retained gate、unsafe diffのfail-closed判定、reuse approvalの世代不一致、quorum判定、configuration invariant conflict、semantic digest、外部contract、controller再起動後の再計算など、失敗時の影響が大きい境界へ最小限のtable test、fuzz test、契約テストを追加する。設定ファイルの存在確認やmock呼出し順だけのテストは追加しない。生成、format、build、vet、lint、manifest検証などの静的検証と、実機依存のTalos、storage、reboot、rollback、drain、CAPI minorごとのreplacement不発を検証するE2Eは責務を分けて実行する。
