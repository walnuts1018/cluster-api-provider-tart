package intelmanageability

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	wsmanRequestTimeout = 15 * time.Second
	wsmanResponseLimit  = 2 << 20
)

var (
	// ErrAuthenticationFailedはcredentialまたはDigest認証handshakeの失敗を表す。
	ErrAuthenticationFailed = errors.New("intel manageability authentication failed")
	// ErrConnectionFailedはHTTP layer以下の接続失敗(DNS、TCP、TLS、timeout)を表す。
	ErrConnectionFailed = errors.New("intel manageability connection failed")
	// ErrProtocolはHTTP status、SOAP Fault、XML decodeなどWS-Man応答の異常を表す。
	ErrProtocol = errors.New("intel manageability protocol error")
)

// xmlNodeはWS-Man応答を汎用的に走査するための木構造である。CIMクラスやSOAP要素の厳密なスキーマを型として持たず、必要なlocal nameだけをDFSで検索する。
type xmlNode struct {
	XMLName  xml.Name
	Content  string    `xml:",chardata"`
	Children []xmlNode `xml:",any"`
}

// findはnode配下(自身を含まない)をDFSでlocalNameに一致する最初のnodeを返す。namespaceは問わない。CIMのresponseは実装によりnamespace prefixやschema versionが揺れるため、local nameでの照合に留める。
func (n *xmlNode) find(localName string) *xmlNode {
	for i := range n.Children {
		child := &n.Children[i]
		if child.XMLName.Local == localName {
			return child
		}
	}
	for i := range n.Children {
		if found := n.Children[i].find(localName); found != nil {
			return found
		}
	}
	return nil
}

// clientConfigはClientの接続設定である。credentialの値そのものを保持するため、ログや文字列化に含めてはならない。
type clientConfig struct {
	endpoint           string
	username           string
	password           string
	caData             []byte
	insecureSkipVerify bool
}

// clientはWS-Man SOAP requestの送受信だけを担う低レベルclientである。CIMクラスの意味論はBackend側に閉じ込める。
type client struct {
	endpoint   *url.URL
	username   string
	password   string
	httpClient *http.Client
}

func newClient(config clientConfig) (*client, error) {
	address := strings.TrimSpace(config.endpoint)
	if address == "" {
		return nil, errors.New("intel manageability endpoint is empty")
	}
	parsed, err := url.ParseRequestURI(address)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("invalid intel manageability endpoint %q", address)
	}
	if strings.TrimSpace(config.username) == "" || strings.TrimSpace(config.password) == "" {
		return nil, errors.New("intel manageability username and password are required")
	}

	httpClient, err := newWSManHTTPClient(parsed.Scheme, config)
	if err != nil {
		return nil, err
	}
	return &client{
		endpoint:   parsed,
		username:   config.username,
		password:   config.password,
		httpClient: httpClient,
	}, nil
}

func newWSManHTTPClient(scheme string, config clientConfig) (*http.Client, error) {
	if scheme != "https" {
		// HTTP transportではTLS設定は不要であり、HTTPSへの暗黙昇格やdowngradeは行わない。
		return &http.Client{Timeout: wsmanRequestTimeout}, nil
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default HTTP transport is not a TCP transport")
	}
	transport = transport.Clone()
	rootCAs, err := wsmanRootCAs(config.caData)
	if err != nil {
		return nil, err
	}
	transport.TLSClientConfig = &tls.Config{
		MinVersion:         tls.VersionTLS12,
		RootCAs:            rootCAs,
		InsecureSkipVerify: config.insecureSkipVerify, //nolint:gosec // Hostごとの明示的な設定としてTLS検証無効化を許可する。
	}
	return &http.Client{Transport: transport, Timeout: wsmanRequestTimeout}, nil
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

// getはWS-Transfer Getでresourceを取得し、Body配下を返す。
func (c *client) get(ctx context.Context, resourceURI string) (*xmlNode, error) {
	return c.call(ctx, actionTransferGet, resourceURI, nil)
}

// enumerateAllはWS-Enumerationでresourceの全instanceを列挙する。OptimizeEnumerationで返らなかった場合はPullで続きを取得する。
func (c *client) enumerateAll(ctx context.Context, resourceURI string) ([]*xmlNode, error) {
	body, err := c.call(ctx, actionEnumerate, resourceURI, enumerateBody)
	if err != nil {
		return nil, err
	}
	if items := body.find("Items"); items != nil && len(items.Children) > 0 {
		return nodePointers(items.Children), nil
	}
	enumerationContext := body.find("EnumerationContext")
	if enumerationContext == nil || strings.TrimSpace(enumerationContext.Content) == "" {
		return nil, nil
	}
	pullResponse, err := c.call(ctx, actionPull, resourceURI, pullBody(enumerationContext.Content))
	if err != nil {
		return nil, err
	}
	items := pullResponse.find("Items")
	if items == nil {
		return nil, nil
	}
	return nodePointers(items.Children), nil
}

func nodePointers(nodes []xmlNode) []*xmlNode {
	pointers := make([]*xmlNode, len(nodes))
	for i := range nodes {
		pointers[i] = &nodes[i]
	}
	return pointers
}

// invokeはWS-Man method invocationを実行し、応答Body配下を返す。
func (c *client) invoke(ctx context.Context, resourceURI, method string, params []invokeParam) (*xmlNode, error) {
	action := resourceURI + "/" + method
	return c.call(ctx, action, resourceURI, invokeBody(resourceURI, method, params))
}

func (c *client) call(ctx context.Context, action, resourceURI string, bodyWriter func(*xml.Encoder) error) (*xmlNode, error) {
	messageID, err := newMessageID()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrProtocol, err)
	}
	payload, err := buildEnvelope(c.endpoint.String(), resourceURI, action, messageID, bodyWriter)
	if err != nil {
		return nil, fmt.Errorf("%w: build WS-Man request: %w", ErrProtocol, err)
	}

	data, err := c.postWithDigest(ctx, payload)
	if err != nil {
		return nil, err
	}

	var envelope xmlNode
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("%w: decode WS-Man response: %w", ErrProtocol, err)
	}
	body := envelope.find("Body")
	if body == nil {
		return nil, fmt.Errorf("%w: WS-Man response has no Body", ErrProtocol)
	}
	if fault := body.find("Fault"); fault != nil {
		return nil, fmt.Errorf("%w: SOAP fault: %s", ErrProtocol, faultMessage(fault))
	}
	return body, nil
}

func faultMessage(fault *xmlNode) string {
	if reason := fault.find("Text"); reason != nil && strings.TrimSpace(reason.Content) != "" {
		return strings.TrimSpace(reason.Content)
	}
	if code := fault.find("Value"); code != nil && strings.TrimSpace(code.Content) != "" {
		return strings.TrimSpace(code.Content)
	}
	return "unspecified fault"
}

// postWithDigestはSOAP payloadを送信し、401が返った場合はWWW-AuthenticateからDigest challengeを解析して一度だけ再送する。
func (c *client) postWithDigest(ctx context.Context, payload []byte) ([]byte, error) {
	response, err := c.post(ctx, payload, "")
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusUnauthorized {
		challengeHeader := response.Header.Get("WWW-Authenticate")
		if err := drainAndClose(response.Body); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrConnectionFailed, err)
		}
		if challengeHeader == "" {
			return nil, fmt.Errorf("%w: server did not present a WWW-Authenticate challenge", ErrAuthenticationFailed)
		}
		challenge, err := parseDigestChallenge(challengeHeader)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrAuthenticationFailed, err)
		}
		authorization, err := challenge.authorizationHeader(c.username, c.password, http.MethodPost, c.endpoint.RequestURI())
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrAuthenticationFailed, err)
		}
		response, err = c.post(ctx, payload, authorization)
		if err != nil {
			return nil, err
		}
	}
	defer func() {
		_ = drainAndClose(response.Body)
	}()

	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w: HTTP status %d", ErrAuthenticationFailed, response.StatusCode)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: HTTP status %d", ErrProtocol, response.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, wsmanResponseLimit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read response: %w", ErrConnectionFailed, err)
	}
	if len(data) > wsmanResponseLimit {
		return nil, fmt.Errorf("%w: response exceeds the size limit", ErrProtocol)
	}
	return data, nil
}

func (c *client) post(ctx context.Context, payload []byte, authorization string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %w", ErrProtocol, err)
	}
	request.Header.Set("Content-Type", "application/soap+xml;charset=UTF-8")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConnectionFailed, err)
	}
	return response, nil
}

func drainAndClose(body io.ReadCloser) error {
	_, copyErr := io.Copy(io.Discard, body)
	closeErr := body.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
