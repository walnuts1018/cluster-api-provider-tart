# 開発ガイド

## 開発環境

Go、Kubebuilder、controller-gen、kustomize、lint toolのversionは`mise.toml`で管理する。個別にtoolを導入せず、リポジトリのtaskを入口にする。`mise.toml`の`[tools]`はRenovateの組み込み`mise` managerで更新するため、同じtool versionを別の設定ファイルへ複製しない。

```bash
mise install
MISE_OFFLINE=1 mise run help
```

Goのmodule cacheやbuild cacheをリポジトリ配下へ置く場合も、環境変数名をtask内で明示し、開発者の既存環境を変更しない。新しいtaskを追加する場合は、CIでも同じtaskまたは同じ引数で再現できるようにする。

## 実装の開始点

実装前に次を読む。

1. [アーキテクチャ](architecture.md)
2. [API contract](api-contract.md)
3. [Machine lifecycle](lifecycle.md)
4. [Talos連携](talos.md)
5. [Cluster API skill](../../.agents/skills/cluster-api/SKILL.md)
6. [Reconcile skill](../../.agents/skills/reconcile/SKILL.md)
7. [Talos skill](../../.agents/skills/talos/SKILL.md)

Provider APIはInfrastructure、Bootstrap、Control Planeをそれぞれ`infrastructure.cluster.x-k8s.io/v1alpha1`、`bootstrap.cluster.x-k8s.io/v1alpha1`、`controlplane.cluster.x-k8s.io/v1alpha1`へ新規に定義する。CAPI coreの現行v1beta2 contract、Talos machineryの型とsemantics、controller-genの生成物を確認し、旧`v1beta1`実装を温存するためのcompatibility layerを作らない。

## 生成と静的確認

CRD、RBAC、DeepCopy、manifestsを変更した場合は、実装で使用するcontroller-genとkustomizeのtaskを実行する。生成物は手編集せず、入力の型またはmarkerを修正して再生成する。

通常のコード変更では、次の順で確認する。

```bash
MISE_OFFLINE=1 mise run fmt
MISE_OFFLINE=1 mise run generate
MISE_OFFLINE=1 mise run manifests
MISE_OFFLINE=1 mise run build
MISE_OFFLINE=1 mise run lint
git --no-pager diff --check
```

実際のtask名が変更された場合は、この文書とCI workflowを同じ変更で更新する。生成、build、lintが旧`domain`、`infrastructure`、agent、artifact packageを参照しないことを確認する。

## Go testの扱い

新設計を組み立てる現在の作業では、新しいGo testを追加せず、`go test`も実行しない。`mise`のdefault task、build task、lint task、CI workflowへGo testを暗黙に含めない。

この方針を解除するときは、先に[検証方針](verification.md)と[gotest skill](../../.agents/skills/gotest/SKILL.md)を更新し、対象を外部契約や破壊的変更を防ぐ重要な判断へ限定する。設定ファイルの存在確認やmock呼出し順だけのテストは追加しない。

## 実装のルール

- `internal`と`pkg`を作らず、責務を直接表すルート直下のpackageへ置く。
- controllerにTalos version比較、Host allocation、quorum計算、update safetyなどのdomain判断を詰め込まない。
- Talos API、power、boot、Kubernetes APIの副作用はadapterまたはcontrollerの明確な境界へ閉じ込める。
- Statusをworkflowのstep番号にせず、observed stateとConditionをserver-side applyで更新する。
- Talosが安全に扱えない差分はMachine replacementやdisk wipeへfallbackせず、blocked Conditionで停止する。
- secret、credential、private key、Bootstrap Dataをlog、Event、Status、metrics label、build artifactへ出力しない。

## 変更とコミット

API型、controller、adapter、manifest、docs、skillなど同じ設計目的の変更を一つのまとまりとして扱う。変更後は[検証方針](verification.md)の静的検証を行い、差分に旧設計の参照が残っていないことを確認する。コミットメッセージは日本語で書き、`--signoff`を付ける。
