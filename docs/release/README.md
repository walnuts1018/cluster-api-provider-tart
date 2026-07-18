# 対応状況

このページは、Tart で利用できる機能の現在の公開状況を示します。機械可読な正本は
[release-matrix.yaml](release-matrix.yaml)です。

## 公開状況

| 状態 | 意味 |
|---|---|
| Supported | 利用者向けに提供し、定められた検証を満たしている機能 |
| Experimental | 検証目的で提供する機能。互換性や復旧手順に制約があります |
| Planned | 設計または実装中であり、利用者向けには提供していない機能 |

現時点で Supported の組合せはありません。本番環境での利用はしないでください。

## Experimental

次の更新機能は試験的に扱います。

- worker の OSOnly 更新
- worker の KubernetesBinary 更新
- 3 台以上の control plane の KubernetesBinary 更新
- 単一 control plane の KubernetesBinary 更新

これらの機能には feature gate が必要で、障害時には手動復旧が必要になる場合があります。利用前に
[未リリースノート](../release-notes/unreleased.md)を確認してください。

## Planned

次の初期 Provisioning は提供予定です。

- Ubuntu 24.04 / 26.04、Debian 13 上の amd64 UEFI kubeadm
- Ubuntu 24.04 / 26.04、Debian 13 上の amd64 UEFI k3s
- Ubuntu 24.04 / 26.04、Debian 13 上の amd64 UEFI k0s

各組合せは OS Artifact、Platform Profile、end-to-end 検証が揃うまで利用者向けには公開しません。
