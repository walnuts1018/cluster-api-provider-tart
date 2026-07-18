# 実機 Provisioning overlay

このoverlayは、隔離したProvisioning L2へ接続された管理クラスタnode上でcontrollerを実行する。
適用前に[Ubuntu 24.04 kubeadm実機導入](../../docs/installation/ubuntu-kubeadm.md)のSecret、ConfigMap、
Agent Artifact配置手順を完了する。

`hostPath`はnode固有であるため、`tart.walnuts.dev/provisioning-network=true`を付けたnodeへ
Agent Artifactを配置する。Deploymentを別nodeへ移動させる場合は、先に同じ検証済みArtifactを
配置してからnode labelを移す。
