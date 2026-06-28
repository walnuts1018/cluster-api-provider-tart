# ADR 0006: digest固定artifactと単回Bootstrap bundleを使用する

- Status: Accepted
- Date: 2026-06-28

## Context

可変URLや`latest`から取得したimageをdiskへ書くと、再現性、監査、改ざん検知がない。Bootstrap Dataにはcluster credentialが含まれ、iPXE kernel parameterやURL queryへ長期tokenを入れるとログ・監視装置へ漏れる。NoCloudの複数endpoint取得と「1回で失効」は相性が悪い。

## Decision

- OS、agent、一時OSをdigest付きOCI Artifactで参照する。
- CIでprovenance、SBOM、署名、dm-verity root hashを生成し、controllerとagentの両方でpolicyを検証する。
- Bootstrap Dataは必要なmetadataとまとめた1つのbundleとしてHTTPSで返す。
- tokenは短命、高entropy、host/operationへbindingし、server側にはhashだけを保存する。
- 正常なbundleレスポンスを1回完了した時点でtokenを消費する。
- agentはbundleをStateへ原子的に配置し、初回boot unitが一度だけ実行する。
- 実行後は原本を削除または暗号化保管し、完了digestだけを残す。
- 受理済みgenerationをcontrollerとStateへ保持してanti-rollbackを行う。

## Consequences

- 単回利用の意味が明確になり、NoCloudの複数GET問題を避けられる。
- 一時OSがBootstrap Provider固有形式を解釈せずに済む。
- bundleの配置・実行adapterはkubeadm/k3sごとにOS image内へ必要になる。
- 署名鍵のrotationとtrust policy運用が必要になる。

## Rejected alternatives

- URLだけをCRDに保存する: 内容を固定できない。
- tokenをquery parameterで長期使用する: 漏洩面とreplay期間が広い。
- token取得完了をMachine Readyとする: OS起動とcluster参加を確認していない。
