# 未リリースノート

## 概要

この文書は、現在の開発版における利用者向けの変更点と制約を示します。公開状況の正本は
[対応状況](../release/README.md)です。

## 試験的な更新機能

worker と control plane の OSOnly / KubernetesBinary 更新は Experimental です。利用する場合は、
対象 Host のディスクと workload の復旧手順を事前に確認し、検証環境で実施してください。

## 既知の制約

- Supported として公開された初期 Provisioning の組合せはまだありません。
- 単一 control plane の `KubernetesBinary` 更新は feature gate なしでは受理しません。
- `StateMigration` の自動復旧は提供していません。`RecoveryRequired` になった場合は手動復旧が必要です。
- controller の再接続を伴う長時間の更新検証は完了していません。
- k3s と k0s の初期 Provisioning および更新は提供していません。

## 導入前の注意

Tart は物理 Host の boot 設定とディスクを操作します。利用するネットワークを一般利用 LAN から分離し、
消去してよいディスクだけを対象にしてください。
