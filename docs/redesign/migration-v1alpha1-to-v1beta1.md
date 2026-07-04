# v1alpha1からv1beta1への移行

## 現在の公開状態

変換Webhookと変換関数を実装し、v1beta1は`served=true`、`storage=false`として公開する。既存controllerはv1alpha1を使用しているため、移行完了までv1alpha1をstorage versionとして維持する。

v1beta1へ変換したobjectは、新しい必須fieldを構造的に満たす。ただし、旧APIから意味を決定できない値には移行用placeholderを設定する。placeholderを含むobjectをProvisioningへ使用してはならない。

v1beta1からv1alpha1 storageへ変換されたobjectには`cluster.x-k8s.io/conversion-data` annotationが付く。現行v1alpha1 Controllerはこのannotationを持つobjectを旧Provisioning flowで処理せず、新Controllerへ接続するまで変更せず保持する。

## 自動変換

| v1alpha1 | v1beta1 |
|---|---|
| `TartCluster.spec.controlPlaneEndpoint` | 同名field |
| `TartCluster.status.initialization.provisioned` | 同名field |
| `TartHost.spec.bootMacAddress`。空の場合は`macAddress` | `spec.identifiers.bootMACAddress` |
| `TartHost.status.machineRef` | `spec.consumerRef` |
| `TartHost.status.state` | `status.phase` |
| `TartMachine.spec.providerID` | 同名field |
| `TartMachine.spec.failureDomain` | `status.failureDomain` |
| `TartMachine.status.hostRef` | 同名field。UIDを維持 |
| `TartMachine.status.initialization.provisioned` | 同名field |
| Template内のlabel、annotation | `spec.template.metadata` |

v1beta1にだけ存在するfieldは、v1alpha1へ一時的に変換する際に`cluster.x-k8s.io/conversion-data` annotationへ保存し、v1beta1へ戻す時に復元する。

## 自動変換で意味を維持できないfield

次のfieldは移行前にoperatorが確認する。

| 対象 | field | 移行時の扱い |
|---|---|---|
| TartCluster | `status.ready` | `initialization.provisioned`から再構成 |
| TartCluster | `status.initialization.bound`、`controlPlaneReady` | v1beta1では削除 |
| TartCluster | `spec.artifactPolicy.allowedRegistries` | `migration.invalid`を設定。実際のregistryへ置換必須 |
| TartHost | `spec.macAddress`と`bootMacAddress`の区別 | v1beta1ではboot MACだけを移行 |
| TartHost | architecture、firmware、Platform Profile、root device hint、Driver | `amd64`、`UEFI`、`legacy-v1alpha1/v1`、最小size 1 byte、`wol`、`ipxe`を設定。inventory確認後に置換必須 |
| TartMachine | `spec.image` | URLから算出したdigestを持つ`oci://migration.invalid/legacy@sha256:...`へ変換。実Artifact digestへ置換必須 |
| TartMachine | `spec.kernelParams`、`initrd`、`bootstrap` | v1beta1では削除 |
| TartMachine | `status.bootstrapSecretName`、`provisioningStartTime`、`tokenExpiresAt`、`consumedBootstrapTokenHash` | 新Operation/Session modelへ引き継がず削除 |
| TartMachine | `status.ready` | `initialization.provisioned`から再構成 |
| TartMachineTemplate | providerID、failureDomain、image、kernel、initrd、bootstrap | providerIDはTemplateから除外し、failureDomainは再選択する。image等はTartMachineと同じ扱い |

## storage version切り替え手順

1. 全TartClusterへ実際の`allowedRegistries`を設定する。
2. 全TartHostへinventory、Platform Profile、root device hint、Driver設定を入力する。
3. 全TartMachineとTemplateのimageをdigest固定OCI参照へ置換する。
4. controllerをv1beta1 APIと新Operation modelへ切り替える。
5. v1alpha1から`storageversion` markerを削除してv1beta1へ追加する。
6. controller-genでCRDを再生成し、変換WebhookがReadyになった後にCRDを適用する。
7. Storage Version Migratorまたは全objectのno-op updateで保存済みobjectをv1beta1へ書き直す。
8. `status.storedVersions`とobject一覧からv1alpha1保存objectが0件であることを確認する。

切り替え前後で[`api/v1alpha1/testdata/tartmachine-v1alpha1.yaml`](../../api/v1alpha1/testdata/tartmachine-v1alpha1.yaml)を変換し、UID付き参照、providerID、provisioned状態が維持されることを確認する。
