# Task 05: OS成果物ビルド

## 目的

A/B slotへ安全に書ける再現可能なOS成果物と、一時Provisioning OSをCIで生成する。

## 依存

- Task 01で確定したfilesystem、mount、boot方式

## 実装範囲

- Ubuntu 24.04 amd64 filesystem slot image
- State/Data mount unitとdistribution adapter
- kubeadm/containerd/kubeletのversion固定
- Provisioning Agent入りの小さなkernel/initramfs
- artifact manifest、state schema、CPU requirement、dm-verity metadata
- digest、署名、SBOM、provenance
- OCI registryへのimmutable publish
- image builder/tool versionのpin
- Kubernetes SIGs Image Builder、mkosi/systemd-repart、独自変換の比較spike

`current`、`latest`、Git branch `main`、未固定の`curl | sh`をbuild inputにしない。whole-disk raw imageとslot filesystem imageを別media typeで識別する。

Image BuilderはUbuntu raw targetとKubernetes用Ansible roleを再利用候補とするが、A/B、dm-verity、Debian 13、upgrade semanticsを満たすと仮定しない。採用する場合はrelease tagへ固定し、upstream patch量を成果として記録する。

## 受け入れ条件

1. 同じsource、lock、toolchainから機能的に同一のmanifestと追跡可能な成果物を生成する。
2. manifestにOS、architecture、filesystem、size、Kubernetes distribution/version、state schema、CPU levelがある。
3. x86-64-v1相当の対象hardwareで起動するbuild設定を明示する。
4. 署名不正、digest不一致、size超過、state schema不適合をagentが書き込み前に拒否できる。
5. rootをread-onlyで起動し、必要なwrite pathがState/Dataへ向く。
6. block改変をdm-verityで検出できる。
7. buildとpublishをmise taskから呼び出せる。
8. 3つのbuilder案を同じmanifest/boot testで比較し、ADR 0009をAcceptedまたはRejectedへ更新する。

## テスト

- QEMU bootとapplication behaviorの統合テスト
- artifact parser/verificationのGo単体テスト
- build証跡のCI artifact保存

Workflowやsample fileが存在することだけを確認するテストは追加しない。

## 対象外

- Ubuntu 26.04、Debian 13、k3s
- Raspberry Pi firmware image

## 関連

- ADR 0003、0006
- ADR 0009
- Issue #147
