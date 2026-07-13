# TODO

適切なタイミングで行うべきこと

- [ ] プロビジョニングプラン（Plan）の自動生成と署名フローの実装（現状は `placeholderPlanDigest` を使用）
- [ ] Provisioning Agentの検証（`verity root hash` や `boot trial metadata`）と通常Agent実行フローの結合
- [ ] HA（高可用性）構成・複数レプリカ化時のDHCP/TFTPネットワーク競合の解決やLeader Electionの考慮
- [ ] シングルショット配信（ワンタイムトークン）時の通信瞬断エラーリカバリの対応（トークン消費後のダウンロード再開不可の解決）
- [ ] Private Registry用のCredential情報のコントローラ設定対応
- [ ] Kubeadm RuntimeのNode Health監視の細分化（`static Pod`, `etcd quorum`, `API health` の個別観測への分割）
- [ ] wireの代わりにkessokuを使う
