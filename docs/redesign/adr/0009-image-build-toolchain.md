# ADR 0009: Image Builderを評価するが、最終成果物builderとは未決定とする

- Status: Proposed
- Date: 2026-06-28

## Context

Kubernetes SIGsのImage BuilderはCAPI向けVM imageをPackerとAnsibleで生成し、raw providerではUbuntu 24.04/26.04のwhole-disk imageを作成できる。Kubernetes binaryのversion固定やCAPI向け設定は再利用価値がある。

一方、本Providerが必要とする成果物はA/B slot用filesystem、dm-verity metadata、State/Data mount契約、anti-rollback manifestである。Image Builderはupgrade/downgrade semanticsをNon-Goalとしており、Debian 12/13のOVA/raw対応は2026年6月時点で未実装のIssueである。raw成果物を生成できることだけでは、この契約を満たさない。

## Decision

Task 05のspikeで次の3案を同じ受け入れ条件により比較する。

1. release tagへ固定したImage Builder raw成果物を変換する。
2. Image BuilderのAnsible roleだけを再利用し、slot layoutとverityは独自に生成する。
3. mkosi/systemd-repart等で独自pipelineを構築し、Kubernetes package設定だけを別定義する。

現時点では3を有力候補としつつ、検証前に最終決定しない。Image Builderの`main` branch、可変URL、`latest`をproduction inputにしない。

## Acceptance gate

- Ubuntu 24.04 amd64で同じ成果物契約と比較testを実行できる。
- whole-disk imageを経由せず、または安全で決定的な変換により、OS/Verity slotを生成できる。
- x86-64-v1、State mount、standard CABPK cloud-configを検証できる。
- package、toolchain、base imageを固定し、SBOM、provenance、署名を生成できる。
- upstream追随コストと独自patch量を測定できる。

Ubuntu 26.04、Debian 13、arm64への移植性はTask 11のrelease gateとし、Task 05の採用判断を不必要にブロックしない。

## Consequences

- Image Builderを一律に却下せず、Kubernetes設定資産を活用できる。
- raw imageが生成できるという理由だけでA/B要件を満たしたと誤認しない。
- 独自pipelineを選ぶ場合、Kubernetes version skewとpackage repository設定を自ら保守する必要がある。

## References

- [Kubernetes SIGs Image Builder](https://github.com/kubernetes-sigs/image-builder)
- [Building Raw Images](https://image-builder.sigs.k8s.io/capi/providers/raw)
- [Image Builder Debian 12/13 support issue](https://github.com/kubernetes-sigs/image-builder/issues/2019)
