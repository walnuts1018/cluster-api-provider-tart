# 対応version matrix

Tartの対応範囲は「最新version」ではなく、各releaseで実際に検証したCAPI、Talos、Kubernetesの組み合わせで定義する。Runtime SDKとInPlaceUpdatesはexperimental featureであり、未検証の組み合わせへ安全なin-place updateを暗黙に拡張しない。

## Releaseごとの記載

各releaseでは、release artifactと同じ変更から次のmatrixを更新する。

| Tart release | CAPI minor | Talos minor | Kubernetes version range | in-place update | 備考 |
| --- | --- | --- | --- | --- | --- |
| 未定義 | 未定義 | 未定義 | 未定義 | 未検証 | 実装完了前は利用可能なreleaseなし |

ここでいうtestedは、CRD/RBAC/managerの静的確認だけではなく、該当するCAPI minorとTalos minorを使ったFresh machine、Discovery、single node、HA control plane、worker、OS/config update、Kubernetes upgrade、deletion、controller restartの受け入れ確認を意味する。未実施の境界はtestedと記載しない。

各CAPI minorで、`CanUpdateMachineSet`または`CanUpdateMachine`がunsafe、unknown、partial diffを`Failure`として返したとき、CAPIの実際の挙動がMachineSet、Machine、TartHost claimを一つも作成しないことを必須の安全E2Eとする。これを確認できないCAPI minorは、Tartのrelease compatibilityへ含めない。静的な`Failure`判定やRuntime Extension endpointの疎通だけではこの契約を満たしたことにしない。

## 判定規則

- CAPI coreのcontract version、Runtime SDKのhook schema、`RuntimeSDK`/`InPlaceUpdates` feature gateの組み合わせがmatrix外なら、Update Extensionはmutable updateを`UnsupportedVersionCombination`として拒否する。
- CAPIの`Failure`がTartの意図に反してimmutable rolloutへ進む組み合わせでは、`CanUpdate*`の正しさだけに依存せず、そのCAPI minorを未対応として拒否する。
- Talos minorがmatrix外なら、Talos configuration、installer、upgrade、`upgrade-k8s`のsemanticsを推測せず、初回provisioningまたはupdateを`Ready=False`、`Reason=UnsupportedVersionCombination`として拒否する。
- Kubernetes version range外、またはTalosが要求するupgrade pathを確認できない場合は、OS updateとKubernetes updateを開始しない。
- `latest`、可変tag、未固定のschematicだけを対応versionの根拠にしない。可能な範囲でdigestと`TartMachine.spec.talosImage`の`{version, schematicID}`を使用する。
- matrixの変更はCAPI、Talos、Kubernetesの依存versionを管理するRenovate設定と同じ更新単位でレビューし、実測なしにtested欄を更新しない。

## CAPI endpointとprovider prerequisites

in-place updateを使うreleaseは、CAPIの`RuntimeSDK=true`と`InPlaceUpdates=true`、TartのHTTPS `ExtensionConfig`、TLS Secret、server certificate、必要なCA trustを前提とする。in-place update hookへ登録できるextensionが一つに制限されるCAPIでは、Tart以外のextensionと同時に登録しない。

Control plane endpointはmatrixのprovider機能ではなく、利用者、IPAM、または別Infrastructure ProviderがCAPI `Cluster.spec.controlPlaneEndpoint`へ設定する。TartはVIPのallocateやkube-vipの導入を対応versionの一部として所有しない。

## 参照する公式contract

- [CAPI ControlPlane contract](https://cluster-api.sigs.k8s.io/developer/providers/contracts/control-plane)
- [CAPI InfraMachine contract](https://cluster-api.sigs.k8s.io/developer/providers/contracts/infra-machine.html)
- [CAPI BootstrapConfig contract](https://main.cluster-api.sigs.k8s.io/developer/providers/contracts/bootstrap-config.html)
- [CAPI in-place update hooks](https://cluster-api.sigs.k8s.io/tasks/experimental-features/runtime-sdk/implement-in-place-update-hooks.html)
- [CAPI experimental features](https://main.cluster-api.sigs.k8s.io/tasks/experimental-features/experimental-features.html)
- [Talos Image Factory](https://docs.siderolabs.com/talos/v1.13/learn-more/image-factory)
