# Task 01: 基礎検証とADR確定

## 目的

設計全体を成立させる高リスクな仮説を、production codeの大量変更前に実証する。

## 対象

- CAPI v1.13.1のRuntimeSDK/InPlaceUpdates feature gate、KCP、MachineDeploymentのhook順序
- Ubuntu 24.04 amd64のfilesystem slot image
- dm-verity read-only rootとState/Dataのsystemd mount
- UEFI boot trial、成功確定、試行回数超過rollback
- systemd-boot boot countingとGRUB fallbackの比較、および採用方式の決定
- kubeadm Bootstrap DataをStateから一度だけ実行する方式
- agent書き込み中、boot metadata更新中、初回boot中の電源断
- stable fallbackとしてのdelete-first host reuse

## 成果物

- `test/fixtures`ではなく、再現可能なQEMU検証用mise taskと必要な実装用fixture
- hook request/responseとCAPI version/feature gateの検証記録
- partition table、mount path、bootloader state遷移の確定案
- ADR 0002、0003のAccepted/Rejected更新
- 対応できない条件を記載したsupport matrix

## 受け入れ条件

1. read-only rootのUbuntu 24.04でcontainerd、kubelet、network、時刻同期、ログが再起動後も動く。
2. OS dataまたはverity metadataの改変を検出して対象slotのcommitを拒否する。
3. `/etc/kubernetes`、kubelet state、etcd dataを保持したslot切替を実証する。
4. 新slotが起動不能な場合、operator操作なしで旧slotが起動する。
5. 3つの電源断pointから旧slotまたは再開可能なoperationへ収束する。
6. `CanUpdate*`が対象外差分を覆わず、CAPIが置換経路を選べる。
7. Runtime Extension再起動後も同一operation IDで`UpdateMachine`を再開できる。
8. 単一ノードは「実験的」とし、API server停止期間を含む成功/rollback条件を記録する。

## 対象外

- production CRDの確定
- Redfish実機対応
- Raspberry Pi対応

## 関連

- ADR 0002、0003、0007
- Issue #143、#147
