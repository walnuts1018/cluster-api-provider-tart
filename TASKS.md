# 今後の実装タスク一覧

AIによるセッションごとの開発を円滑に行うため、リリースまでに必要なタスクをフェーズごとに分割し、GitHubのIssueとして定義しました。各AIセッションでは、これらのIssueを1つずつ（または関連するものをまとめて）消化していく想定です。

## Phase 2: ネットワークブート基盤
物理PCがネットワークブート（PXEブート）するためのDHCP/TFTPサーバーと、iPXEスクリプトの配信機能を実装します。

- [ ] [#5 dnsmasqのdocker image](https://github.com/walnuts1018/cluster-api-provider-tart/issues/5)
- [ ] [#6 [Phase 2] Bootstrapper (DHCP/TFTP) と ProxyDHCP 対応の実装](https://github.com/walnuts1018/cluster-api-provider-tart/issues/6)
- [ ] [#7 [Phase 2] TartMachine用 iPXE スクリプトの動的生成・配信機能の実装](https://github.com/walnuts1018/cluster-api-provider-tart/issues/7)

## Phase 3: メタデータサーバーと汎用化
CAPIコントローラーから受け取ったBootstrap Secret（機密情報を含む設定データ）を、セキュアに物理PCに渡すための仕組みを構築します。

- [ ] [#8 [Phase 3] Metadata/Assets HTTPサーバーの構築](https://github.com/walnuts1018/cluster-api-provider-tart/issues/8)
- [ ] [#9 [Phase 3] セキュリティロジック (One Time Token, Time Window) の実装](https://github.com/walnuts1018/cluster-api-provider-tart/issues/9)

## Phase 4: CAPI 統合とテスト
作成したプロバイダーを使って、実際にKubernetesクラスターが立ち上がるかのテスト（E2E）と、継続的インテグレーション（CI）環境を整備します。

- [ ] [#10 [Phase 4] CAPI 統合テストの実装 (Ubuntu + Kubeadm)](https://github.com/walnuts1018/cluster-api-provider-tart/issues/10)
- [ ] [#11 [Phase 4] CAPI 統合テストの実装 (Talos Linux)](https://github.com/walnuts1018/cluster-api-provider-tart/issues/11)
- [ ] [#12 [共通] CI/CD パイプラインの構築](https://github.com/walnuts1018/cluster-api-provider-tart/issues/12)

---

## 開発の進め方
1. `TASKS.md` に記載されたタスク（Issue）の中から、未完了のものを選びます。
2. 関連するIssue番号を含むブランチ（例: `feature/issue-6-proxy-dhcp`）を作成します。
3. 実装とテストを行い、コミットします（`--signoff` 必須）。
4. `gh` コマンドを使用してPull Requestを作成し、レビューまたはマージのステップへ進めます。
5. マージされたら、このファイルのチェックボックスを更新してください。