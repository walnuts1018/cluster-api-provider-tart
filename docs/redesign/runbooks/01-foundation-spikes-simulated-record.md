# Task 01: Foundation Spikes Simulated Record

この記録は GitHub Actions `OS Artifact` workflow と repository 内 test による
Task 01 の継続検証記録である。QEMU 上の bootloader 実証を含むが、実機差分の代替にはしない。

## 実行 command

```bash
mise run artifact-test-dm-verity
mise run artifact-test-boot-trial-rollback
mise run artifact-test-boot-trial-metadata-persistence
mise run artifact-test-bootloader-rollback
mise run artifact-test-firstboot-qemu
go test ./hack/os-firstboot-qemu -v
go test ./hack/boot-trial-rollback-sim -v
```

## 確認する内容

1. dm-verity の正常系と block 改変失敗系を、同じ mkosi 成果物に対して継続監視する。
2. rollback 判定ロジックは simulator evidence として分離し、bootloader 実装そのものの代替に使わない。
3. boot metadata 更新直後の強制停止後も、次回 boot で trial 情報を再読込できることを確認する。
4. OVMF + systemd-boot の bootloader 経路で、target entry の tries が 3 回消費され、
   4 回目に rollback entry が選択されることを確認する。
5. first-boot QEMU で、read-only root mount と first-boot BootReport 送信を継続監視する。

## CI 証跡の 3 系統

acceptance 8 の CI 証跡は、同じ rollback という言葉でも役割が異なるため、次の 3 系統へ分けて扱う。

### 1. rollback 判定系

- workflow step: `Verify boot trial rollback simulator evidence`
- task: `mise run artifact-test-boot-trial-rollback`
- artifact:
  - `dist/os-artifact/boot-trial-rollback/evidence.json`
  - `dist/os-artifact/boot-trial-rollback/summary.txt`

この系統は「3 回失敗したら旧 slot へ戻す」という状態遷移と判定条件だけを固定する。
bootloader の rename、電源断後の metadata 永続化、実際に選択された boot entry の証明には使わない。

### 2. metadata 永続化系

- workflow step: `Verify boot metadata persistence after forced power loss`
- task: `mise run artifact-test-boot-trial-metadata-persistence`
- artifact:
  - `dist/os-artifact/boot-trial-metadata-persistence/evidence.json`
  - `dist/os-artifact/boot-trial-metadata-persistence/serial-boot1.log`
  - `dist/os-artifact/boot-trial-metadata-persistence/serial-boot2.log`

この系統は boot metadata を永続ディスクへ書いた直後に強制停止し、次回 boot で同じ trial 情報を
再読込できることを確認する。rollback 判定や bootloader entry 選択の代替ではない。

### 3. boot 選択実証系

- workflow step: `Verify bootloader rollback after three failed target boots`
- task: `mise run artifact-test-bootloader-rollback`
- artifact:
  - `dist/os-artifact/bootloader-rollback/evidence.json`
  - `dist/os-artifact/bootloader-rollback/serial-boot1.log`
  - `dist/os-artifact/bootloader-rollback/serial-boot2.log`
  - `dist/os-artifact/bootloader-rollback/serial-boot3.log`
  - `dist/os-artifact/bootloader-rollback/serial-boot4.log`

この系統は OVMF + systemd-boot の実 bootloader 経路で、`tart-target+3.conf` が 1st から 3rd boot で
`+2-1`、`+1-2`、`+0-3` へ変化し、4th boot で `tart.qemu.boot-entry=rollback` が選択されることを確認する。
acceptance 8 のうち「4 回分の boot 順序」「4 回目で target を再選択しない」はこの系統で追う。

## repository 内で確認できる主な test

- `hack/os-firstboot-qemu/main_test.go`
  - `TestBootEntrySelectionFromLogは選択されたEntryを読む`
  - `TestQEMUBootloaderRollbackScriptはKernelCmdlineから選択Entryを出力する`
  - `TestBootloaderEntryConfigは指定rootと識別子を埋め込む`
- `hack/boot-trial-rollback-sim`
  - rollback 判定入力と状態遷移の evidence 生成

## 2026-07-18 時点で CI に残している完了証跡

- `OS Artifact` workflow の build evidence artifact
- `dist/os-artifact/dm-verity/*`
- `dist/os-artifact/boot-trial-rollback/*`
- `dist/os-artifact/boot-trial-metadata-persistence/*`
- `dist/os-artifact/bootloader-rollback/*`
- `dist/os-artifact/qemu-firstboot/*`
- `OS Artifact` workflow run `29630838351`
- `CI` workflow run `29630838422`

## 2026-07-18 時点の補足

`OS Artifact` workflow run `29630838351` で、`Verify bootloader rollback after three failed target boots`
まで含めて成功した。これで acceptance 8 の CI 証跡は、rollback 判定系、metadata 永続化系、
boot 選択実証系の 3 系統がそろった。

## なお CI で未代替の残差

1. QEMU で確認した bootloader 挙動と、実機 firmware 実装差分が一致すること。
2. slot rootfs の切替と Health Gate を含む end-to-end の rollback 完走。
3. Task 01 の全 acceptance を満たした後に、`amd64-uefi-ab/v1` を Supported 扱いへ上げてよいかの最終判断。

これらは CI で同じ失敗分類を追う証跡を維持したまま、必要な残差だけを実機記録へ分離する。
