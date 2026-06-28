# Task 07: 初期プロビジョニング

## 目的

Ubuntu 24.04 + amd64 + UEFI + WoL + kubeadmのCAPI縦方向スライスを完成させる。

## 依存

- Task 02、06

## 実装範囲

- Host選択、予約、network boot、disk初期化、OS-A配置
- Bootstrap bundleのStateへの原子的配置
- first-boot systemd unitによるBootstrap Dataの一度だけの実行
- providerID、addresses、initialization、Conditions
- boot confirmationとNode Readyの確認
- deletion policy、clean/release、stableなdelete-first host reuse
- controller/agent/host再起動からのoperation再開

Bootstrap Providerの出力をInfrastructure Providerがkubeadm設定として再解釈しない。実行adapterはOS image側に置く。

## 受け入れ条件

1. CAPI cluster作成からNode Readyまで手操作なしで到達する。
2. Machine Readyはdisk writeではなく、対象slot起動、bootstrap成功、providerID/Node確認後に成立する。
3. controller再起動、agent再送、WoL重複で二重partitionや二重bootstrapが起きない。
4. Bootstrap Secret/tokenが完了後に不要な場所へ残らない。
5. Machine削除後、policyに応じてState/Dataを保持またはcleanし、hostを再利用できる。
6. `maxSurge=0`等のdelete-first構成で、同じ物理hostを新Machineへ再割当できる。
7. 通常のCAPI置換更新がRuntime Extension無効時にも動く。

## 検証

- ローカルでは単体・統合・QEMUを実行する。
- `mise run test-provisioning-e2e`はGitHub Actions上で実行する。
- 最低1台の実機でdisk identityと再起動を検証する。

## 対象外

- 同一Node identityを維持するA/B更新
- k3s、他OS、Redfish

## 関連

- ADR 0001、0004、0006
- Issue #145、#147

