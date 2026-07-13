# Task 05: OS Artifact Security Simulated Record

この記録はrepository内testによる疑似検証であり、Task 05のQEMU/実機検証やrelease運用の代替ではない。

## 実行command

```bash
go test ./internal/server/agentboot ./internal/provisioningagent/artifactfetch ./internal/provisioningagent/writer ./internal/provisioningagent/client -v
```

## 確認する内容

1. controllerは署名検証とpayload digest検証に成功したAgent Artifactだけを配信し、検証後にpathを差し替えても配信内容を変えない。
2. Agentは署名済みPlanと署名済みOS Artifactだけを受理し、信頼していない署名鍵を拒否する。
3. Artifact payloadが改変された場合、controller配信前またはAgent write前に拒否する。

## repository内で確認できる主なtest

- `internal/server/agentboot/handler_test.go`
  `TestLoadArtifactは署名とPayload検証後だけArtifactを返す`
- `internal/provisioningagent/artifactfetch/oci_test.go`
  `TestOCIFetchVerifiesMetadataBeforeReturningPayloads`
  `TestOCIFetchRejectsPlanIdentityMismatchBeforePayloadFetch`
- `internal/provisioningagent/writer/writer_test.go`
  `WriteTargets() accepted an untrusted Artifact` を拒否するcase
- `internal/provisioningagent/client/client_test.go`
  `FetchPlan() accepted an untrusted signature key`

## なお未検証の項目

- lock file固定入力での2回build一致
- dm-verity block改変検出
- read-only root bootとState/Data bind mount
- x86-64-v1 QEMU boot
- release用署名鍵の運用手順
