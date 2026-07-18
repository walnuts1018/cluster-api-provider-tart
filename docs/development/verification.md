# 検証方針

変更は可能な限り GitHub Actions で再現できる形で検証する。ローカル環境や実機で一度成功したことだけを
完了の根拠にしない。

## 検証レイヤー

| 対象 | 主な検証 | 実行場所 |
|---|---|---|
| Domain の判断・状態遷移 | Go unit test | CI 必須 |
| Driver と Agent protocol | Contract Test / Simulator | CI 必須 |
| Kubernetes controller | envtest | CI 必須 |
| Manifest と生成物 | `mise run manifests` / `mise run generate` | CI 必須 |
| 静的解析 | `mise run ci-lint` | CI 必須 |
| Kind E2E | `mise run test-e2e` | GitHub Actions |
| Provisioning E2E | `mise run test-provisioning-e2e` | GitHub Actions |
| OS disk / boot | QEMU task | CI を優先 |
| 実機固有の挙動 | 実機検証 | CI では代替できない部分だけ |

## 実機検証の扱い

実機が必要な検証では、機種、firmware、NIC、storage controller、使用した Driver、失敗を注入した位置、
合格条件を記録する。同時に、protocol、状態遷移、失敗分類など実機に依存しない部分は Contract Test または
Simulator Test で CI に固定する。

## E2E と artifact

E2E の失敗時には、対象 Resource の Condition、Event、構造化 log、QEMU log など原因を判別するための
artifact を保存する。credential、Bootstrap Data、Secret の値、PVC payload 自体は保存してはならない。

更新機能を検証する場合は、更新前後の Resource UID と PVC payload digest を比較する。Pod 名、
`resourceVersion`、Node 名は更新で変化し得るため、同一性の判定に使用しない。

## 確認の基準

新しい変更では、最小限でも次を満たす。

1. 重要な Domain 判断または外部契約を検証するテストがある。
2. 生成物を変更した場合は生成 task を実行している。
3. CI で実行できない検証には、その理由と実機での合格条件が記録されている。
4. ログや artifact に機密情報が含まれない。
