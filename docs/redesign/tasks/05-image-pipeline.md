# Task 05: OS Artifact Build

## 目的

Ubuntu 24.04 amd64のOS/Verity SlotとProvisioning Agentを、固定入力からCIで再生成し、署名付きOCI Artifactとして公開する。

## 依存

- Task 01
- ADR 0003、0006、0009

## 入力

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

## 実装状況（2026-07-05）

Task 01のboot方式比較は保留したまま、Task 04を解放するManifest契約とmkosi案の実装を先行した。

実装済み:

- `pkg/artifact`にManifest v1のGo型、検証済み型、RFC 8785 canonical JSON、digest計算を追加した。
- Ed25519によるManifest署名・検証と、payloadのsize/SHA-256事前検証を追加した。
- `artifact/schema/os-manifest-v1.schema.json`にProtocol共有用JSON Schemaを追加した。
- Ubuntu snapshot、mkosi v26 commit、Kubernetes v1.35.0 amd64 debのURL・size・SHA-256をlock fileへ固定した。
- lock fileに一致する入力だけをatomicに配置するdownload処理とmise taskを追加した。
- mkosi/systemd-repartで8 GiB ext4 rootとdetached dm-verity hashを分離出力する設定を追加した。
- mkosi package manifestからCycloneDX 1.6 SBOMを生成する処理を追加した。
- Artifact Manifestとlock fileからSLSA provenance v1を生成する処理を追加した。
- OS/Verity/kernel/initrd、Manifest、署名、SBOM、provenanceをOCI Artifactへまとめ、digest固定参照を出力する処理を追加した。
- Linux上のbuild確認とGHCR公開を行うGitHub Actions workflowを追加した。

未検証・未実装:

- mkosi案のLinux build結果、成果物名、2回buildの一致はGitHub Actionsで検証中。
- dm-verity block改変検出、read-only root boot、x86-64-v1 QEMU bootはTask 01の保留に伴い未検証。
- State/Data bind mount契約とbootloader方式はTask 01の測定結果がないため確定していない。
- Image Builder raw変換案とAnsible role再利用案の比較は未実施で、ADR 0009は`Proposed`のままとする。
- Provisioning Agent Artifact、release用署名鍵の運用、controller/Agent双方への署名検証接続は未実装。

mkosi設定の`Bootloader=systemd-boot`はkernel/initrdをbuild成果物として取り出すための暫定値であり、物理diskの既定bootloader採用を意味しない。
Task 01のboot trial検証後にPlatform Profileのbootloaderと一致させること。
