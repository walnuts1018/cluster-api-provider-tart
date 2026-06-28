# ADR 0006: digest固定artifactと単回Bootstrap bundleを使用する

- Status: Accepted
- Date: 2026-06-28

## Context

可変URLや`latest`から取得したimageをdiskへ書くと、再現性、監査、改ざん検知がない。Bootstrap Dataにはcluster credentialが含まれ、iPXE kernel parameterやURL queryへ長期tokenを入れるとログ・監視装置へ漏れる。NoCloudの複数endpoint取得と「1回で失効」は相性が悪い。

## Decision

- OS、Agent、一時OSを`sha256:<64桁>`付きOCI Artifactで参照する。tagだけの参照をAdmissionで拒否する。
- CIでprovenance、SBOM、Manifest署名、dm-verity root hashを生成する。controllerはPlan生成前、Agentはdisk書き込み前に署名とdigestを検証する。
- Bootstrap Dataは必要なmetadataとまとめた1つのbundleとしてHTTPSで返す。
- bundleはBootstrap Secretの`value`、`format`、payload digest、Machine UID、operation IDを保持する。
- Session Tokenは256 bit以上、TTL 10分とし、Host UIDとOperation UIDへbindingする。管理クラスタにはSHA-256 hashだけを保存する。
- Bundle HTTP responseのheaderとbodyをserverが送信完了した時点でtokenを消費する。Client受信結果が不明でも同じtokenを再有効化しない。
- AgentはBundleを一時fileへ書き、fsync後にrenameしてStateへ配置する。
- Bootstrap Adapterはpayload digestの成功markerが存在する場合、payloadを再実行しない。
- 実行成功後はpayload原本を削除し、payload digest、Adapter version、適用時刻だけを残す。暗号化保管は初期リリースでは実装しない。
- 受理済みgenerationをcontrollerとStateへ保持してanti-rollbackを行う。
- MVPはstandard CABPK `cloud-config`をOS内adapterで適用し、Ignitionや未分離pathへ書く任意customizationは拒否する。
- initial credentialはTPM/事前登録host key/BMC保護mediaを優先し、それらを持たないnetwork boot hostは隔離L2を必須の脅威モデルとする。

## Consequences

- 単回利用の意味が明確になり、NoCloudの複数GET問題を避けられる。
- 一時OSがBootstrap Provider固有形式を解釈せずに済む。
- bundleの配置・実行adapterはkubeadm/k3sごとにOS image内へ必要になる。
- hardware identityを持たないLegacy BIOS機では、悪意あるprovisioning L2参加者からBootstrap Secretを完全には保護できない。
- 署名鍵のrotationとtrust policy運用が必要になる。

## Rejected alternatives

- URLだけをCRDに保存する: 内容を固定できない。
- tokenをquery parameterで長期使用する: 漏洩面とreplay期間が広い。
- token取得完了をMachine Readyとする: OS起動とcluster参加を確認していない。
