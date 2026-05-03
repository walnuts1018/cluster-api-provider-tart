# Cluster API Infrastructure Provider (Desktop Bare-Metal) 設計書

## 1. プロジェクト概要と背景 (Context)

本プロジェクトは、IPMIやBMCなどの高度な管理インターフェースを持たない「一般的なデスクトップ物理PC」を対象とした、Cluster API (CAPI) のカスタム Infrastructure Provider を開発することを目的とする。

### 1.1. 設計の基本方針

* **OS / Bootstrap 非依存**: Kubeadm (Ubuntu等) と Talos Linux の両方、および将来的な他のOSにコードの変更なしで対応できる「汎用的なプロビジョニング」を実現する。
* **Pull型プロビジョニング**: SSH接続によるスクリプト実行(Push型)ではなく、物理PCが自ら設定を取得しにくるメタデータサーバー方式(Pull型)を採用する。
* **オールインワン・マネジメント**: コントローラー、DHCPサーバー、TFTPサーバー、HTTPサーバーの全コンポーネントをマネジメントクラスタ (Kubernetes) 上で稼働させる。

---

## 2. アーキテクチャとコンポーネント

マネジメントクラスタ内の1つのNamespace（例: `capd-system`）に、以下のコンポーネントをデプロイする。物理ネットワークへのL2到達性（DHCPブロードキャストやWoL送信のため）を確保するため、必要に応じて `hostNetwork: true` または LoadBalancer を利用する。

### 2.1. 構成要素

1. **Infrastructure Controller (The Brain)**
   * CRDの監視、PCの割り当て、WoL(Wake-on-LAN)による電源投入、ワンタイムトークンの発行を担う。
2. **Bootstrapper (DHCP / TFTP Server)**
   * `dnsmasq` などを利用。PXEブート要求に対し、IPアドレスの払い出しと iPXE ブートローダ (`ipxe.efi`) を提供する。
3. **Assets & Metadata Server (HTTP Server)**
   * **Assets**: OSのカーネルイメージ (vmlinuz) や initrd、動的な iPXE スクリプトを配信する。
   * **Metadata**: クラスタの管理者権限を含む Bootstrap Data (Secret) を安全に配信する。

---

## 3. CRD (Custom Resource Definition) 設計

インベントリ管理とCAPIリソースのマッピングを行うため、2つのCRDを定義する。

### 3.1. DesktopHost (インベントリ管理)

物理PC 1台につき1つのリソースを作成し、ハードウェア情報を管理する。

* **Spec**:
  * `macAddress`: 物理NICのMACアドレス (必須)
  * `bootMacAddress`: PXEブートに使用するNICのMACアドレス (複数NICがある場合)
* **Status**:
  * `state`: `Available`, `Reserved`, `Provisioning`, `Provisioned`
  * `machineRef`: 割り当てられている `DesktopMachine` の参照

### 3.2. DesktopMachine (CAPI インフラリソース)

CAPI の `Machine` リソースと1対1で対応する。

* **Spec**:
  * `image`: ブートするOSイメージのURL、または Assets サーバー内のパス。
  * `kernelParams`: iPXEに渡すカーネルパラメータのテンプレート。
    * 例: `talos.config=https://{{.ServerIP}}/metadata/{{.MacAddress}}?token={{.Token}}`
* **Status**:
  * `ready`: プロビジョニングが完了したか (Boolean)
  * `token`: セキュア配信用の One Time Token
  * `provisioningStartTime`: タイムアウト判定用のタイムスタンプ

---

## 4. プロビジョニング・ワークフロー

CAPIから対象マシンの作成要求 (`Machine` の作成) が来てからのフロー。

1. **[Controller]** 空いている `DesktopHost` を検索し、`DesktopMachine` と紐付ける (`State: Reserved`)。
2. **[Controller]** CAPI コアコントローラーが生成した Bootstrap Secret (`cloud-init` や `MachineConfig` を含む) を取得。内容は解釈せず、単なるバイト列 (`[]byte`) として扱う。
3. **[Controller]** 対象マシン用の One Time Token を生成し、CRDに保存。
4. **[Controller]** 対象の MAC アドレス宛に Wake-on-LAN (Magic Packet) を送信。状態を `Provisioning` に変更し、10分間のアクセス許可タイマーを開始。
5. **[Physical PC]** 電源が入り、PXE ブートを実行。Bootstrapper (DHCP) からIPを取得し、iPXE をロード。
6. **[Physical PC (iPXE)]** HTTPサーバーへ iPXE スクリプトを要求。
7. **[Metadata Server]** MACアドレスとトークンを埋め込んだカーネルパラメータを含む起動スクリプトを生成して返す。
8. **[Physical PC (OS)]** OSカーネルがロードされ、起動。カーネルパラメータで指定された Metadata Server のURL (トークン付き) にアクセス。
9. **[Metadata Server]** セキュリティ検証 (後述) を行い、合格すれば Bootstrap Data を返却。
10. **[Physical PC (OS)]** Kubeadm または Talos 等が実行され、クラスタにノードとして参加。

---

## 5. セキュリティ設計 (Bootstrap Data の保護)

Bootstrap Dataには証明書などの機密情報が含まれるため、以下の多層防御により「正しい物理PC」以外へのデータ漏洩を防ぐ。

1. **ワンタイム・トークン (Unguessable URL)**:
   * メタデータ取得URLには、推測不可能な One Time Token を必須とする。
   * 例: `GET /metadata/00:1A:2B:3C:4D:5E?token=a1b2c3d4...`
2. **時間的制約 (Time Window)**:
   * WoL送信から一定時間 (例: 10分間) のみアクセスを許可する。期限切れの場合は 403 Forbidden を返す。
3. **シングルショット配信 (One-Time Delivery)**:
   * 物理PCが一度でも正常にメタデータをダウンロードしたら、コントローラーは即座にそのトークンを無効化 (破棄) する。リプレイ攻撃を防止する。
4. **IP/MAC 照合 (オプション)**:
   * リクエスト元の送信元IPアドレスが、DHCPサーバーがそのMACアドレスに払い出したIPと一致するかを検証する。
5. **TLS暗号化 (推奨)**:
   * ネットワーク上でのパケットキャプチャを防ぐため、メタデータサーバーは自己署名証明書などを用いて HTTPS で稼働させる。

---

## 6. Control Plane の高可用性 (kube-vip) 設計

本プロバイダーは、Layer 2 (ARP) モードでの `kube-vip` 運用を標準シナリオとしてサポートするが、責務を明確に分離する。

* **Infrastructure Provider (本システム) の責務**:
  * Control Plane 用の仮想IP (VIP) を `DesktopCluster` リソースでユーザーから受け取り、CAPIの `status.apiEndpoints` として通知する。
  * ソフトウェアのインストールや設定には介入しない。
* **ユーザー / Template の責務**:
  * Kubeadm や Talos の Bootstrap テンプレート (`KubeadmConfigTemplate` 等) 内で、`kube-vip` の Static Pod マニフェストを配置するよう記述する。
  * L2 モードを使用するか、L3 (BGP) モードを使用するかの決定と設定はテンプレート側で行う。

---

## 7. 実装のフェーズ分け

段階的な開発を推奨する。

* **Phase 1: インベントリ管理と電源操作**
  * `DesktopHost` / `DesktopMachine` CRDの実装。
  * MACアドレスによるWoL送信ロジックの実装。
* **Phase 2: ネットワークブート基盤**
  * `dnsmasq` を使った DHCP/TFTP Pod の構築と、ダミーの iPXE スクリプト配信。
* **Phase 3: メタデータサーバーと汎用化**
  * Bootstrap Secret を Opaque データとして配信する HTTP サーバーの実装。
  * セキュリティ要件 (ワンタイムトークン、シングルショット配信) の実装。
* **Phase 4: CAPI 統合とテスト**
  * Cluster API コアコントローラーとの連携テスト。
  * Ubuntu+Kubeadm、Talos の両テンプレートでのプロビジョニング確認。
