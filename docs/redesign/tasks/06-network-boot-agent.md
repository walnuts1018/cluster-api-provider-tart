# Task 06: Agentとネットワークブート統合

## 目的

既存ProxyDHCP/TFTP/iPXE基盤から一時OSを起動し、Agentが安全にdisk planを実行できるようにする。

## 依存

- Task 03、04、05

## 実装範囲

- amd64 UEFI向けiPXE script生成
- digest固定kernel/initramfs/agent artifact配信
- agentのinventory、disk selection、partition、slot/verity write、read-back verification
- `/dev/disk/by-id`、serial/WWN、sizeによる多条件照合
- operation progressと再開marker
- controller network serverのsingle-active ownership
- disk write、fsync、boot metadata更新のfailure injection

初期化planだけがpartition tableを作成できる。更新planはinactive OS slotと明示したboot metadata以外へ書き込めない。

## 受け入れ条件

1. iPXEから正しいarchitectureの一時OSを起動できる。
2. 候補diskが0件または複数件なら書き込まず停止する。
3. expected serial/sizeが不一致なら明確なConditionを設定する。
4. 同じplanを再実行してもactive slot、State、Dataを破壊しない。
5. download/write途中の再起動後、検証済み境界から再開または安全に最初からやり直す。
6. 書き込み後のfilesystem digestとverity root hashがmanifestに一致しなければboot targetを変更しない。
7. 非leaderまたはstandby replicaが競合するDHCP/TFTP応答をしない。
8. agent完了だけではInfraMachineのinitializationを完了にしない。

## 対象外

- Bootstrap実行とCAPI Ready
- Redfish Virtual Media
- arm64/Raspberry Pi

## 関連

- ADR 0004、0007
- Issue #147
