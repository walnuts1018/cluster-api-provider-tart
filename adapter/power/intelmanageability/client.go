package intelmanageability

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/device-management-toolkit/go-wsman-messages/v2/pkg/amterror"
	"github.com/device-management-toolkit/go-wsman-messages/v2/pkg/wsman"
	"github.com/device-management-toolkit/go-wsman-messages/v2/pkg/wsman/cim/associatedpower"
	"github.com/device-management-toolkit/go-wsman-messages/v2/pkg/wsman/cim/models"
	wsmanpower "github.com/device-management-toolkit/go-wsman-messages/v2/pkg/wsman/cim/power"
	wsmanclient "github.com/device-management-toolkit/go-wsman-messages/v2/pkg/wsman/client"

	"github.com/walnuts1018/cluster-api-provider-tart/domain/endpoint"
)

const wsmanRequestTimeout = 15 * time.Second

var (
	// ErrAuthenticationFailedはcredentialまたはDigest認証handshakeの失敗を表す。
	ErrAuthenticationFailed = errors.New("intel manageability authentication failed")
	// ErrConnectionFailedはHTTP layer以下の接続失敗(DNS、TCP、TLS、timeout)を表す。
	ErrConnectionFailed = errors.New("intel manageability connection failed")
	// ErrProtocolはSOAP Fault、不正なresponse、nonzeroなReturnValueなどWS-Man応答の異常を表す。
	ErrProtocol = errors.New("intel manageability protocol error")
)

// cimPowerStateはDMTF CIM_PowerManagementService::PowerState(DSP1027)の値域である。
// RequestPowerStateChangeの入力にもCIM_AssociatedPowerManagementService.PowerStateの観測値にも使う。
// go-wsman-messages/v2 v2.50.3のpkg/wsman/cim/models.PowerStateenumと数値が一致することをソースで確認済みであり、
// 同enumへ乗り換えず独自型に留めるのは、書き込み側のpkg/wsman/cim/power.PowerStateがシンボリック定数を
// 持たない単なるintであるため、読み書き双方で共通に使える型をこのpackage内に1つ用意するためである。
type cimPowerState int

const (
	dmtfPowerOn              cimPowerState = 2
	dmtfPowerOffHard         cimPowerState = 6
	dmtfPowerOffSoft         cimPowerState = 8
	dmtfPowerMasterBusReset  cimPowerState = 10
	dmtfPowerOffSoftGraceful cimPowerState = 12
	dmtfPowerOffHardGraceful cimPowerState = 13
)

// manageabilityClientはBackendが依存する最小限のWS-Man操作である。go-wsman-messagesの型をこのfile外へ
// 漏らさず、Backend側はprotocol実装をtestなしで差し替えられるようにする。
type manageabilityClient interface {
	currentPowerState(ctx context.Context) (cimPowerState, error)
	requestPowerStateChange(ctx context.Context, state cimPowerState) error
}

// goWSManClientはgithub.com/device-management-toolkit/go-wsman-messages/v2を使ってCIM_PowerManagementService/
// CIM_AssociatedPowerManagementServiceだけを操作するclientである。go-wsman-messagesの型はこのfile内に閉じ込め、
// Backendへは渡さない。
type goWSManClient struct {
	messages wsman.Messages
}

func newClient(config Config) (*goWSManClient, error) {
	dialAddress, hostname, useTLS, err := parseEndpoint(config.Address)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.Username) == "" || strings.TrimSpace(config.Password) == "" {
		return nil, errors.New("intel manageability username and password are required")
	}

	// go-wsman-messages/v2のclient.NewWsmanは、cp.Targetとcp.UseTLSからAMTの既定port(16992/16993)
	// 固定でrequest URLを自前構築する。実機がNATやport forwarding越しの非既定portで公開されている場合や、
	// testでの差し替えに対応するため、実際に検証済みのdialAddress(host:port)へ接続先を差し替える
	// DialContextを持つTransportを渡す。req.URLはlibraryが構築したまま(既定port)で送信されるが、
	// TCP/TLSの接続先だけをここで上書きする。
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, dialAddress)
		},
	}
	if useTLS {
		rootCAs, err := wsmanRootCAs(config.CAData)
		if err != nil {
			return nil, err
		}
		transport.TLSClientConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			RootCAs:            rootCAs,
			InsecureSkipVerify: config.InsecureSkipVerify, //nolint:gosec // Hostごとの明示的な設定としてTLS検証無効化を許可する。
		}
	}

	messages := wsman.NewMessages(wsmanclient.Parameters{
		Target:   hostname,
		Username: config.Username,
		Password: config.Password,
		// AMT/Standard ManageabilityはHTTP Digest(RFC 2617)のみを要求するため常に有効化する。
		UseDigest: true,
		UseTLS:    useTLS,
		// credentialや生のWS-Man messageをログへ出力しないため無効のままにする。
		LogAMTMessages: false,
		Timeout:        wsmanRequestTimeout,
		Transport:      transport,
	})
	return &goWSManClient{messages: messages}, nil
}

// parseEndpointはIntel Manageability endpointを検証し、実際に接続すべき"host:port"(dialAddress)、
// go-wsman-messagesのclient.Parameters.Targetに渡すホスト名、TLS要否を返す。
//
// Intel ME 7.1 / Standard Manageabilityの実機はWS-Man endpointを常に"/wsman"直下にのみ公開するため、
// 明示されたpathがそれ以外であれば拒否する(go-wsman-messagesも内部でrequest pathを常に"/wsman"固定で
// 送信するため、実機と整合しないpathを受理しても意味がない)。一方portは、NAT/port forwarding越しの
// 非既定port経由でも実機へ到達できるよう、scheme既定値(http:16992、https:16993)以外も許容する。
func parseEndpoint(address string) (dialAddress, hostname string, useTLS bool, err error) {
	trimmed := strings.TrimSpace(address)
	parsedEndpoint, err := endpoint.ParseHTTPURL(trimmed)
	if err != nil {
		return "", "", false, fmt.Errorf("validate intel manageability address: %w", err)
	}
	parsed, err := url.Parse(parsedEndpoint.String())
	if err != nil {
		return "", "", false, fmt.Errorf("parse intel manageability address: %w", err)
	}

	if path := strings.TrimSuffix(parsed.Path, "/"); path != "" && path != wsmanclient.WSManPath {
		return "", "", false, fmt.Errorf("intel manageability address %q must use the default WS-Man path %s", trimmed, wsmanclient.WSManPath)
	}

	useTLS = parsed.Scheme == "https"
	hostname = parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = wsmanclient.NonTLSPort
		if useTLS {
			port = wsmanclient.TLSPort
		}
	}
	return net.JoinHostPort(hostname, port), hostname, useTLS, nil
}

func wsmanRootCAs(data []byte) (*x509.CertPool, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(data) {
		return nil, errors.New("intel manageability CA data does not contain a valid PEM certificate")
	}
	return rootCAs, nil
}

// currentPowerStateはCIM_AssociatedPowerManagementServiceの現在のPowerStateを観測する。
//
// Intel ME 7.1 / Standard Manageabilityの実機は、WS-Enumeration EnumerateへOptimizeEnumerationを
// 要求していなくてもinstanceをEnumerate応答へ直接埋め込んで返し、EnumerationContextを省略することがある
// (Pull不要の1往復応答)。go-wsman-messages/v2の型付きresponse(associatedpower.Body)はこのケースを
// 表現できず、EnumerateResponseからEnumerationContextしか読めないため、まずEnumerate応答の生XMLに
// inline instanceが含まれていないか確認し、無ければEnumerationContextを使ってPullする。
func (c *goWSManClient) currentPowerState(ctx context.Context) (cimPowerState, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	enumerateResponse, err := c.messages.CIM.AssociatedPowerManagementService.Enumerate()
	if err != nil {
		return 0, classifyError(err)
	}

	inlineItems, err := decodeInlineAssociatedPowerManagementServiceItems(enumerateResponse.XMLOutput)
	if err != nil {
		return 0, fmt.Errorf("%w: decode CIM_AssociatedPowerManagementService enumeration response: %w", ErrProtocol, err)
	}
	if len(inlineItems) > 0 {
		return convertPowerState(inlineItems[0].PowerState), nil
	}

	enumerationContext := enumerateResponse.Body.EnumerateResponse.EnumerationContext
	if enumerationContext == "" {
		return 0, fmt.Errorf("%w: CIM_AssociatedPowerManagementService enumeration returned no context", ErrProtocol)
	}

	pullResponse, err := c.messages.CIM.AssociatedPowerManagementService.Pull(enumerationContext)
	if err != nil {
		return 0, classifyError(err)
	}
	items := pullResponse.Body.PullResponse.AssociatedPowerManagementServiceItems
	if len(items) == 0 {
		return 0, fmt.Errorf("%w: no CIM_AssociatedPowerManagementService instance was returned", ErrProtocol)
	}
	return convertPowerState(items[0].PowerState), nil
}

// decodeInlineAssociatedPowerManagementServiceItemsは、EnumerateResponse直下にinstanceが直接埋め込まれた
// 応答(Pull不要の1往復応答)からCIM_AssociatedPowerManagementServiceを読み取る。通常のPull応答と同じ
// 型(associatedpower.CIM_AssociatedPowerManagementService)を再利用し、Itemsの置き場所だけが異なる
// EnumerateResponseの生XMLを別途decodeする。
func decodeInlineAssociatedPowerManagementServiceItems(rawXML string) ([]associatedpower.CIM_AssociatedPowerManagementService, error) {
	var envelope struct {
		Body struct {
			EnumerateResponse struct {
				Items struct {
					AssociatedPowerManagementServiceItems []associatedpower.CIM_AssociatedPowerManagementService `xml:"CIM_AssociatedPowerManagementService"`
				} `xml:"Items"`
			} `xml:"EnumerateResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal([]byte(rawXML), &envelope); err != nil {
		return nil, err
	}
	return envelope.Body.EnumerateResponse.Items.AssociatedPowerManagementServiceItems, nil
}

// convertPowerStateはgo-wsman-messagesのmodels.PowerState(observed value)をこのpackageのcimPowerStateへ変換する。
// 両者はDMTFの数値をそのまま使う単純なintであるため、変換は値の受け渡しに過ぎない。
func convertPowerState(state models.PowerState) cimPowerState {
	return cimPowerState(state)
}

// requestPowerStateChangeはCIM_PowerManagementService::RequestPowerStateChangeを同期的に要求する。
// ReturnValueが4096(Job Started)などの非同期応答であっても、今回のscopeではjob pollingを行わず成功として扱わない。
func (c *goWSManClient) requestPowerStateChange(ctx context.Context, state cimPowerState) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	response, err := c.messages.CIM.PowerManagementService.RequestPowerStateChange(wsmanpower.PowerState(state))
	if err != nil {
		return classifyError(err)
	}
	return checkReturnValue(response.Body.RequestPowerStateChangeResponse.ReturnValue)
}

func checkReturnValue(returnValue wsmanpower.ReturnValue) error {
	if returnValue == 0 {
		return nil
	}
	return fmt.Errorf("%w: RequestPowerStateChange returned code %d", ErrProtocol, returnValue)
}

// classifyErrorはgo-wsman-messagesが返すerrorを、このpackageが公開するsentinel errorへ分類する。
// SOAP Fault(HTTP 400)は*amterror.AMTErrorへdecodeされ、DNS/TCP/TLS/timeoutは*url.Errorとしてそのまま
// 返るため、両者はerrors.Asで判定できる。一方HTTP 401/403はgo-wsman-messages内部で型無しのfmt.Errorf(
// "wsman.Client post received: <status>\n<body>")としてしか返らず、専用の型やsentinelが存在しない。
// そのためこの1箇所だけはstatus文字列によるfallback判定を行う。
func classifyError(err error) error {
	if err == nil {
		return nil
	}

	if _, ok := errors.AsType[*amterror.AMTError](err); ok {
		return fmt.Errorf("%w: %w", ErrProtocol, err)
	}

	if _, ok := errors.AsType[*url.Error](err); ok {
		return fmt.Errorf("%w: %w", ErrConnectionFailed, err)
	}

	message := err.Error()
	if strings.Contains(message, "wsman.Client post received") {
		if strings.Contains(message, " 401 ") || strings.Contains(message, " 403 ") {
			return fmt.Errorf("%w: %w", ErrAuthenticationFailed, err)
		}
		return fmt.Errorf("%w: %w", ErrProtocol, err)
	}

	return fmt.Errorf("%w: %w", ErrProtocol, err)
}
