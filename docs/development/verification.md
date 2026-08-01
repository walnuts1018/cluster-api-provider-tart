# 検証方針

変更は可能な限り GitHub Actions で再現できる形で検証する。ローカル環境や実機で一度成功したことだけを
完了の根拠にしない。

## 検証レイヤー

| 対象 | 主な検証 | 実行場所 |
|---|---|---|
| Domain の判断・状態遷移 | Go unit test | Push / Pull Request CI |
| Driver と Agent protocol | Contract Test / Simulator | Push / Pull Request CI |
| Kubernetes controller | envtest | Push / Pull Request CI |
| Manifest と生成物 | `mise run manifests` / `mise run generate` | Push / Pull Request CI |
| Provisioning Agent initramfs | module・firmware を含む Artifact 組み立て | Release Artifact workflow |
| 静的解析 | `mise run ci-lint` | Push / Pull Request CI |
| Kind E2E | `mise run test-e2e` | 手動Releaseの検証段階 |
| Provisioning E2E | `mise run test-provisioning-e2e` | 手動Releaseの検証段階 |
| OS disk / boot | QEMU task | Release Artifact workflow |
| 実機固有の挙動 | 実機検証 | CI では代替できない部分だけ |

## GitHub Actions の役割分担

- `CI` はPull Requestと`main`へのpushで、生成、lint、Go test、buildだけを実行する。長時間のE2EやArtifact生成は含めない。
- `Release` は手動実行だけを受け付ける。最初にKind E2EとProvisioning E2Eを並列に実行し、両方が成功した場合だけArtifact生成を開始する。
- `Release Artifacts` はAgent、OS、iPXE、Provider imageを生成・公開し、最後にProvider manifestを作る。GitHub ReleaseはすべてのArtifact生成が成功してから作成する。

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
