# CI検証方針

## 目的

再設計タスクの受け入れ条件を、可能な限りGitHub Actions上で再現可能な検証へ変換する。実機や手元の偶発的な成功を完了証跡の中心にせず、次のAI Agentと人間が同じ手順で退行を確認できる状態を維持する。

## 基本方針

- 新しい受け入れ条件を満たす実装では、最初にCIで再現できる検証形態を選ぶ。
- ローカルでしか実行できない手順を正本にしない。
- `mise run test-e2e` と `mise run test-provisioning-e2e` はローカルで実行せず、GitHub Actions上で実行する。
- QEMUで再現できるdisk、boot、failure injectionは、QEMU taskまたはGitHub Actions workflowへ組み込む。
- 実機Labが必要な検証でも、Protocol、Driver contract、State transition、error分類はCI上のtestで固定する。
- CIで担保できない残差は、対象Task文書とRunbookに「なぜCIで代替できないか」「必要な実機情報」「合格条件」を記録する。

## 検証レイヤー

| Layer | 実行場所 | 完了証跡として認める条件 |
|---|---|---|
| Domain / Application test | CI必須 | `go test`のpackage名とtest名で受け入れ条件を追跡できる |
| Contract / Simulator test | CI必須 | 実DriverまたはProtocolの失敗分類を同じ入力で再現する |
| Kind e2e | CI必須、ローカル禁止 | `mise run test-e2e` のGitHub Actions結果を記録する |
| QEMU disk / boot | CI推奨、ローカル実行可 | CPU model、OVMF version、disk size、Artifact digestを保存する |
| Provisioning e2e | CI必須、ローカル禁止 | `mise run test-provisioning-e2e` のGitHub Actions結果とartifactを保存する |
| Cluster lifecycle e2e | CI必須、ローカル禁止 | 実workload clusterの構築、OSOnly/KubernetesBinary更新、Resource/PVC比較結果を同一runへ保存する |
| 実機Lab | 実機Labのみ | 機種、Firmware、NIC/storage controller、BMC version、failure injection位置を保存する |

## Cluster lifecycleのデータ保持証跡

OSOnlyまたはKubernetesBinary更新の前に、検証用NamespaceへDeployment、StatefulSet、
Service、ConfigMap、Secret、PV、PVCを作成し、PVCへ暗号学的乱数payloadを書き込む。
更新前artifactには各ResourceのapiVersion、kind、namespace、name、UIDとpayloadの
SHA-256 digestを保存する。Secretの値とpayload自体はartifactへ保存しない。

更新完了後は同じResourceをAPIから再取得し、UIDが更新前と一致することを確認する。
PVCをmountした新しいPodからpayload digestを再計算し、更新前と一致することを確認する。
Pod名、resourceVersion、Node名は更新処理で変化し得るため同一性の判定へ使用しない。

Provisioning E2E内の疑似Node Ready、fake Kubernetes client、State/Data roleを対象外にした
Planの単体テストだけでは、この証跡を満たしたと扱わない。実Providerが構築したworkload
clusterと実diskを使い、control plane API停止時間、更新対象Node順、更新前後のKubernetes
version、Artifact digest、Platform Profileを同じartifactへ保存する。

## AI Agent向け運用

1. 未検証項目を見つけたら、まずCIで再現するtestまたはworkflowへ落とせるかを判断する。
2. CIで再現できる場合は、検証用code、workflow、Task文書の完了証跡を同じ変更に含める。
3. CIで完全再現できない場合は、SimulatorまたはContract Testで近い失敗分類を固定し、実機Labでだけ確認する残差をRunbookへ分離する。
4. Simulator rollback evidenceは、bootloader実装そのものや電源断後の永続化挙動を実証する証跡として扱わない。Task 01 の boot trial rollback では、状態遷移と判定条件の継続監視だけを担わせる。
5. 「workflowが存在する」「scriptが存在する」だけを検証するtestは追加しない。
6. 重いE2EはGitHub Actionsのpath filter、manual dispatch、scheduled runを使い分ける。ただしSupportedへ昇格する対象はrelease前に必ず成功結果を残す。
7. OS Artifact workflowは`workflow_dispatch`を維持したまま、`artifact/mkosi`、`hack/os-firstboot-qemu`、artifact manifest/provenance、workflow定義、`mise.toml`、関連Task文書の変更で`push`/`pull_request`でも継続実行する。

## 現時点の優先順位

1. Task 07のfirst-boot QEMU smokeをOS Artifact workflowで`pull_request`と`main` pushに対して継続実行し、CPU model、disk size、Artifact digest、BootReport要約をartifactへ保存する。
2. 実Providerが構築したUbuntu 24.04 kubeadm workload clusterで、OSOnly更新と
   KubernetesBinary更新の前後にKubernetes Resource UID、PV/PVC UID、PVC payload digestを
   比較するCluster lifecycle E2Eを追加する。
3. Task 09の単一control plane `management API outage` を、QEMUまたはk3s管理クラスタ上のGitHub Actions E2Eとして再現する。
4. Task 01のboot trial rollbackについては、CIでsimulator rollback evidence、boot metadata 永続化証跡、boot選択実証証跡を継続収集し、rollback判定に使った入力、期待した状態遷移、Artifact/Profile識別子をartifactへ保存する。これは bootloader 実機/QEMU 実証の代替ではなく、役割分担を分けて残差を減らすための継続証跡とする。
5. `hack/os-firstboot-qemu` の direct-kernel QEMU では、boot metadata を永続ディスクへ書いた直後に強制停止し、次回 boot で読み戻す CI 証跡を別系統で残す。これは storage write-through と再読込の確認であり、simulator rollback evidence の代替でも、bootloader 実装そのものの代替でもない。
6. `hack/os-firstboot-qemu --scenario bootloader-rollback` では、OVMF + systemd-boot の実 bootloader 経路で `tart-target+3.conf` の tries 消費と、4th boot の rollback entry 選択を検証する。artifact には少なくとも `evidence.json` と `serial-boot1.log` から `serial-boot4.log` を保存する。
7. acceptance 8 の CI 証跡要件は、次の3系統で管理する。
   - rollback判定系: simulator rollback evidence として、失敗回数、旧slot復帰判定、期待状態遷移、Artifact/Profile識別子を保存する。
   - metadata永続化系: direct-kernel QEMU として、boot metadata 更新直後の強制停止後も trial counter または同等情報が次回 boot で再読込されることを保存する。
   - boot選択実証系: bootloader を通した QEMU または実機で、1st から 4th boot までの slot 選択結果を保存する。少なくとも 3rd failure 後に旧slotが起動し、4th boot で新slotを再選択しないことが分かる console log、boot menu state、または同等の artifact を要件とする。
8. 上記7のうち boot選択実証系を GitHub Actions にまだ載せられない場合は、Task 文書と Runbook に「CI で未代替の理由」「残っている失敗分類」「利用する QEMU/実機環境」「合格条件」を明記する。
9. Task 07のProvisioning E2EをPRまたは`main` pushで継続実行できる状態に保つ。
10. Task 10のRedfishは実機前にSimulator ContractをCIで維持し、実機差分だけをLab記録へ残す。
11. 実機導入用のKustomize overlayとkubeadm cluster templateは、通常導入、実機導入、Provisioning E2E
    の各Manifestを同じCIでrenderし、templateにKCP設定とcontrol plane/workerのHost selectorが
    含まれることを検証する。これは実機起動の代替ではなく、
    install時にmanagerが初期設定不足で終了する回帰とtemplateの破損を早期に検出するための契約である。

Task 01 の boot trial rollback は、上記 7 の 3 系統を
[Task 01: Foundation Spikes Simulated Record](runbooks/01-foundation-spikes-simulated-record.md)
へ集約し、その文書を CI 証跡の役割分担の正本として扱う。
