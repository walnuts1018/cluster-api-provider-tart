# Task 10: 対応拡大とリリース

## 目的

完成した縦方向スライスを対象OS、distribution、boot platform、architectureへ段階的に広げ、運用可能なリリースにする。

## 依存

- Task 08、09

## 拡張順序

1. Debian 13 + amd64 UEFI + kubeadm
2. Ubuntu 26.04 + amd64 UEFI + kubeadm
3. k3s用Bootstrap/Control Plane Providerとの統合
4. amd64 Legacy BIOS
5. arm64 UEFI
6. Raspberry Pi 4/5専用profile

k3sのcluster初期化やupgradeロジックをInfrastructure Providerへ追加しない。対応するBootstrap/Control Plane Providerを選定または別コンポーネントとして設計する。

## 実装範囲

- profileごとのslot image、mount path、bootloader、health adapter
- OS/Kubernetes upgrade matrixとstate schema
- Legacy BIOS boot partitionとrollback
- arm64 artifact
- Raspberry Pi EEPROM/network boot/firmware partitionの専用adapter
- backup/restore、failed update、host replacementのrunbook
- deprecated flow削除とconversion完了
- `.agent/skills/architecture/SKILL.md`、`AGENTS.md`、利用者文書の同期

## 受け入れ条件

1. support matrixの各supported組合せに初期導入、再起動、更新、rollback、削除・再利用の証跡がある。
2. Ubuntu 26.04のartifactが対象とする古いamd64 CPU levelで起動する。
3. k3sのNode/cluster identityとデータpathがslot更新後も保持される。
4. Legacy BIOSとRaspberry PiをUEFI成功だけでsupported扱いしない。
5. unsupported組合せをadmissionまたはpreflightで早期拒否する。
6. State/Data backupと復元を実行できるrunbookがある。
7. 旧installer/whole-disk flowの利用objectがないことを移行toolで確認してから削除する。
8. release noteにexperimentalなin-place updateのfeature gateとfallbackを明記する。

## 対象外

- 全Redfish vendorの保証
- 任意Linux distribution
- 汎用WASM runtime
- storage system自体のapplication-consistent backup実装

## 関連

- ADR 0002、0003、0005、0007
- Issue #143、#146、#147

