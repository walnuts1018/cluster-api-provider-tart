---
name: architecture
description: プロジェクトのアーキテクチャ設計とコンポーネント構成の確認
when_to_use: 新しいコンポーネントを設計・実装する時や、プロビジョニングフロー、CRDの仕様を確認する時
---

# Cluster API Infrastructure Provider (Desktop Bare-Metal) 設計方針

## 基本方針

- **OS / Bootstrap 非依存**: Kubeadm (Ubuntu等) と Talos Linux の両方、および将来的な他のOSにコードの変更なしで対応できる汎用的なプロビジョニング。
- **Pull型プロビジョニング**: SSH接続によるPush型ではなく、物理PCが自ら設定を取得しにくるメタデータサーバー方式。
- **オールインワン・マネジメント**: コントローラー、DHCPサーバー、TFTPサーバー、HTTPサーバーの全コンポーネントをマネジメントクラスタ (Kubernetes) 上で稼働させる。

## コンポーネント構成

1. **Infrastructure Controller**: CRDの監視、PCの割り当て、WoL(Wake-on-LAN)による電源投入、ワンタイムトークンの発行を担う。
2. **Bootstrapper (DHCP / TFTP Server)**: `dnsmasq` などを利用しPXEブート環境を提供。既存ネットワークへの影響を防ぐため **ProxyDHCP** をサポートする。
3. **Assets & Metadata Server (HTTP Server)**:
   - Assets: カーネルイメージ、initrd、iPXEスクリプトを配信。
   - Metadata: ワンタイムトークンを用いたセキュアなBootstrap Data(Secret)の配信。

## CRD設計

- **TartHost**: 物理PCのインベントリ管理（MACアドレスとステータス `Available`, `Reserved`, `Provisioning`, `Provisioned` を保持）。
- **TartMachine**: CAPIの `Machine` と1対1。OSイメージURLやカーネルパラメータ、セキュア配信用ワンタイムトークン(UUID)を保持。

## セキュリティ設計 (メタデータ配信)

- 推測不可能なUUIDによる **ワンタイム・トークン** を使用。
- WoL送信から **10分間の時間的制約** を設定。
- 一度取得されたら無効化する **シングルショット配信** によりリプレイ攻撃を防止。
