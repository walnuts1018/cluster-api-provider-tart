---
name: architecture
description: プロジェクトのアーキテクチャ設計とコンポーネント構成の確認
when_to_use: 新しいコンポーネントを設計・実装する時や、プロビジョニングフロー、CRDの仕様を確認する時
---

# Cluster API Infrastructure Provider (Desktop Bare-Metal) 設計方針

## 基本方針

- **OS / Bootstrap 非依存**: Kubeadm (Ubuntu等) と Talos Linux の両方、および将来的な他のOSにコードの変更なしで対応できる汎用的なプロビジョニング。
- **Pull型プロビジョニング**: SSH接続によるPush型ではなく、物理PCが自ら設定を取得しにくるメタデータサーバー方式。
- **Monolithic Controller (オールインワン・マネジメント)**: コントローラー、DHCPサーバー、TFTPサーバー、HTTPサーバーを複数のコンテナに分けるのではなく、単一のGoバイナリ（コントローラープロセス）内にすべて組み込んで実装する。

## コンポーネント構成

1. **Infrastructure Controller (The Brain)**: 単一のPod (`hostNetwork: true`) で稼働し、以下のサブシステムをGoroutineとして並行起動する。
   - **K8s Reconciler**: CRDの監視、PCの割り当て、WoL(Wake-on-LAN)による電源投入、ワンタイムトークンの発行を担う。
   - **Embedded DHCP Server**: `insomniacslk/dhcp` を利用しProxyDHCPとして実装。既存ネットワークへの影響を防ぎつつPXEブート環境を提供。クライアントのアーキテクチャ (DHCP Option 93) を検出し、`ipxe-x86_64.efi` または `ipxe-arm64.efi` を適切に選択して応答。
   - **Embedded TFTP Server**: `pin/tftp` を利用しiPXEバイナリを配信。
   - **Embedded HTTP Server**: カーネル/initrd、動的iPXEスクリプト、ワンタイムトークンを用いたセキュアなBootstrap Data(Secret)を配信。MACアドレスに基づくTartHost検索と動的スクリプト生成をサポート。

   ### ネットワークブートシーケンス
   1. **ProxyDHCP**: 物理PCがDHCP Discoverを送信 → 組み込みDHCPサーバーがクライアントのアーキテクチャを判別し、適切なiPXEブートローダのパスを応答。
   2. **TFTP**: 物理PCがiPXEブートローダをTFTPで取得 → 組み込みTFTPサーバーが要求されたバイナリ (`ipxe-x86_64.efi`等) を配信。これらのバイナリは ORAS を用いて OCI Artifact としてパッケージングされ、Kubernetesの Image Volume Mount 機能を用いてTFTPルートにマウントされる。

   3. **HTTP (iPXE Script)**: 物理PCが動的iPXEスクリプトを取得 → HTTPサーバーがMACアドレスに基づいてTartHost/TartMachineを検索し、適切なカーネル/initrd/パラメータを生成

4. **Kernel Boot**: iPXEスクリプトに従ってカーネルを起動 → Talos LinuxまたはUbuntu/Kubeadmがブート

## CRD設計

- **TartHost**: 物理PCのインベントリ管理（MACアドレスとステータス `Available`, `Reserved`, `Provisioning`, `Provisioned` を保持）。
- **TartMachine**: CAPIの `Machine` と1対1。OSイメージURLやカーネルパラメータ、セキュア配信用 One Time Token を保持。

## セキュリティ設計 (メタデータ配信)

- 推測不可能な **One Time Token** を使用。
- WoL送信から **10分間の時間的制約** を設定。
- 一度取得されたら無効化する **シングルショット配信** によりリプレイ攻撃を防止。
