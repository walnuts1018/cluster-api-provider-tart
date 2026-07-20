# 実機 Provisioning overlay

このoverlayは、隔離したProvisioning L2へ接続された管理クラスタnode上でcontrollerを実行する。
適用前に[Ubuntu 24.04 kubeadm実機導入](../../docs/installation/ubuntu-kubeadm.md)のConfigMapと
Agent Artifact配置手順を完了する。Agent Plan署名鍵とAgent API証明書はProvider Podの起動時に自動生成する。
初期化コンテナだけが共有`emptyDir`の所有権を確定するためrootで実行される。managerは非rootのままで、
秘密鍵はUID 0、GID 65532、mode `0440`としてマウントされる。

Agent Artifactのimage volumeは、GitHub Releaseの`infrastructure-components.yaml`だけに含める。release workflowは
`github.ref_name`をArtifactタグとして埋め込む。`config/real-hardware`を直接`kustomize build`して導入することはできない。
`mise run build-installer-real-hardware`を使う場合も、同じrelease tagを`AGENT_ARTIFACT_REF`へ指定する。

`hostPath`はnode固有であるため、`tart.walnuts.dev/provisioning-network=true`を付けたnodeへ
Agent Artifactを配置する。Deploymentを別nodeへ移動させる場合は、先に同じ検証済みArtifactを
配置してからnode labelを移す。
