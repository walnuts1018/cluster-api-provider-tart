# ADR 0009: Image Builderを評価するが、最終成果物builderとは未決定とする

- Status: Proposed
- Date: 2026-06-28

## Context

Kubernetes SIGsのImage BuilderはCAPI向けVM imageをPackerとAnsibleで生成する。2026-06-28時点のraw provider文書にはUbuntu 24.04のbuild commandがあり、Ubuntu 26.04 raw targetの欠落はopen Issue #2044として追跡されている。Kubernetes binaryのversion固定やCAPI向け設定は再利用価値がある。

一方、本Providerが必要とする成果物はA/B slot用filesystem、dm-verity metadata、State/Data mount契約、anti-rollback manifestである。Debian 12/13のOVA/raw対応は2026-06-28時点でopen Issue #2018/#2019として追跡されている。raw成果物を生成できることだけでは、この契約を満たさない。

## Decision

Task 05で次の3案を同じUbuntu 24.04 amd64入力と同じQEMU boot testで比較する。

1. release tagへ固定したImage Builder raw成果物を変換する。
2. Image BuilderのAnsible roleだけを再利用し、slot layoutとverityは独自に生成する。
3. mkosiとsystemd-repartで独自pipelineを構築し、Kubernetes package設定だけを別定義する。

現時点では3を第一候補とするが、検証前に確定しない。Image Builderの`main` branch、可変URL、`latest`をproduction inputにしない。

## Acceptance gate

- 3案全てで同じManifest schemaを生成する。生成できない案は不採用とする。
- OS/Verity payload digestがlock fileで指定した入力から再生成できる。
- x86-64-v1、State mount、standard CABPK cloud-configを検証できる。
- package、toolchain、base imageを固定し、SBOM、provenance、署名を生成できる。
- upstreamから変更したfile数、patch行数、build時間、Artifact sizeを比較表へ記録する。

Ubuntu 26.04、Debian 13、arm64への移植性はTask 11のrelease gateとし、Task 05の採用判断を不必要にブロックしない。

全機能条件を満たす案が複数ある場合は、独自patch行数が最小の案を採用する。差が10%以内ならbuild時間が短い案を採用する。

## Consequences

- Image Builderを一律に却下せず、Kubernetes設定資産を活用できる。
- raw imageが生成できるという理由だけでA/B要件を満たしたと誤認しない。
- 独自pipelineを選ぶ場合、Kubernetes version skewとpackage repository設定を自ら保守する必要がある。

## Alternatives

- Image Builder raw成果物を無条件に採用する: whole-disk imageの生成だけではA/B filesystem、dm-verity、State/Data契約を満たさないため却下。
- Image Builderを評価対象から除外する: Kubernetes package設定とCAPI向けの既存Ansible roleを再利用できる可能性を検証せず失うため却下。
- Ubuntu/Debianのinstaller ISOを毎回実行する: 更新時間、入力の再現性、inactive slotだけを書き換える保証を共通化できないため却下。

## References

- [Kubernetes SIGs Image Builder](https://github.com/kubernetes-sigs/image-builder)
- [Building Raw Images](https://image-builder.sigs.k8s.io/capi/providers/raw)
- [Image Builder Ubuntu 26.04 raw target issue](https://github.com/kubernetes-sigs/image-builder/issues/2044)
- [Image Builder Debian 12+ OVA/raw support issue](https://github.com/kubernetes-sigs/image-builder/issues/2018)
- [Image Builder Debian 12/13 support issue](https://github.com/kubernetes-sigs/image-builder/issues/2019)
