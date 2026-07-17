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
| 実機Lab | 実機Labのみ | 機種、Firmware、NIC/storage controller、BMC version、failure injection位置を保存する |

## AI Agent向け運用

1. 未検証項目を見つけたら、まずCIで再現するtestまたはworkflowへ落とせるかを判断する。
2. CIで再現できる場合は、検証用code、workflow、Task文書の完了証跡を同じ変更に含める。
3. CIで完全再現できない場合は、SimulatorまたはContract Testで近い失敗分類を固定し、実機Labでだけ確認する残差をRunbookへ分離する。
4. 「workflowが存在する」「scriptが存在する」だけを検証するtestは追加しない。
5. 重いE2EはGitHub Actionsのpath filter、manual dispatch、scheduled runを使い分ける。ただしSupportedへ昇格する対象はrelease前に必ず成功結果を残す。
6. OS Artifact workflowは`workflow_dispatch`を維持したまま、`artifact/mkosi`、`hack/os-firstboot-qemu`、artifact manifest/provenance、workflow定義、`mise.toml`、関連Task文書の変更で`push`/`pull_request`でも継続実行する。

## 現時点の優先順位

1. Task 07のfirst-boot QEMU smokeをOS Artifact workflowで`pull_request`と`main` pushに対して継続実行し、CPU model、disk size、Artifact digest、BootReport要約をartifactへ保存する。
2. Task 09の単一control plane `management API outage` を、QEMUまたはk3s管理クラスタ上のGitHub Actions E2Eとして再現する。
3. Task 01のA/B、read-only root、dm-verity、boot trial rollbackをCIで継続実行し、QEMU first-boot証跡とdm-verity改変logへCPU modelとArtifact digestを紐付けて保存する。
4. Task 07のProvisioning E2EをPRまたは`main` pushで継続実行できる状態に保つ。
5. Task 10のRedfishは実機前にSimulator ContractをCIで維持し、実機差分だけをLab記録へ残す。
