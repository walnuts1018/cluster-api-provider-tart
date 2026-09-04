# 実機設定

TartはTalos Linux専用Providerとして再実装中です。旧Provisioning Agent、Ubuntu/kubeadm、独自OS artifactを前提にしたこのoverlayの導入手順は廃止しました。

実機で検証する場合は、まず[Machine lifecycle](../../docs/development/lifecycle.md)、[Talos連携](../../docs/development/talos.md)、[セキュリティと観測性](../../docs/development/security.md)、[検証方針](../../docs/development/verification.md)を確認してください。実用可能なmanifestが成立した時点で、このoverlayを新しいProviderのpower/boot backendに合わせて再構成します。
