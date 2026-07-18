# cluster-api-provider-tart

Tart は、一般的な物理 PC を Cluster API で管理するための Infrastructure Provider です。PXE と
Provisioning Agent を使い、管理クラスタから物理 Host の初期導入を行います。

> [!WARNING]
> このプロジェクトは開発中であり、現時点では本番利用をサポートしていません。操作によって対象 Host の
> ディスクが消去されるため、検証専用の隔離ネットワークと再利用してよいディスクだけを使用してください。

## はじめに

- [ドキュメント](docs/README.md): 導入手順
- [Ubuntu 24.04 と kubeadm の実機導入](docs/installation/ubuntu-kubeadm.md): 現在提供している検証用導入手順

## 開発への参加

設計方針、開発手順、CI 検証は [開発者向けドキュメント](docs/development/README.md) にまとめています。
