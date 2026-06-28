# Task 05: OS Artifact Build

## 目的

Ubuntu 24.04 amd64のOS/Verity SlotとProvisioning Agentを、固定入力からCIで再生成し、署名付きOCI Artifactとして公開する。

## 依存

- Task 01
- ADR 0003、0006、0009

## 固定入力

- Ubuntu releaseとrepository snapshot
- package名とversion
- Kubernetes/containerd/kubelet version
- kernel/initramfs package version
- Build toolとversion
- Platform Profile ID
- `stateSchema`

全入力をrepository内のlock fileへ保存する。`latest`、`current`、Git `main`、version未指定の`curl | sh`を禁止する。

## 成果物

- OS filesystem payload
- Verity payload
- Artifact Manifest
- SBOM
- provenance
- Manifest署名
- Provisioning Agent kernel/initramfsまたはISO
- OCI publish/verify用mise task
- Image Builder 3案比較表

## Manifest必須field

- `schemaVersion`
- `mediaType`
- OS family/version
- architecture/CPU level
- filesystem
- OS/Verity digestとbyte数
- verity root hash
- stateSchema min/max
- Kubernetes distribution/version
- kernel/initrd digest
- Artifact Generation
- Platform Profile ID

## 受け入れ条件

1. lock file以外を変更せず2回buildし、package一覧、Manifest field、payload内file一覧が一致する。
2. OCI参照がdigest固定であり、tagを変更しても取得内容が変わらない。
3. Manifest signatureを変更したcaseをcontrollerとAgentの両方が拒否する。
4. OS payloadを1 byte変更したcaseをdigest検証で拒否する。
5. 書き込み後のblockを1 byte変更したcaseをdm-verityが検出する。
6. rootをread-only mountし、Platform Profileの全State/Data pathがbind mountになる。
7. x86-64-v1 CPU modelでbootする。
8. SBOMへ全OS packageとGo binary moduleを記録する。
9. Image Builder raw変換、Ansible role再利用、mkosi/systemd-repart案のpatch行数、build時間、Artifact sizeを記録する。
10. ADR 0009の選択規則に従いStatusを`Accepted`または`Rejected`へ更新する。

## 完了証跡

- lock file
- 2回分のbuild logとManifest
- OCI digest
- signature verification log
- QEMU boot log
- SBOM/provenance
- 3案比較表

## 対象外

- Ubuntu 26.04
- Debian 13
- arm64
- k3s
- Raspberry Pi firmware

## 関連

- ADR 0003、0006、0009
- Issue #147
