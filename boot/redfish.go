package boot

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/walnuts1018/cluster-api-provider-tart/domain/endpoint"
)

const (
	redfishRequestTimeout = 15 * time.Second
	redfishResponseLimit  = 2 << 20
)

// RedfishConfigはRedfish backendへ渡す解決済みの接続設定である。Secretの参照名ではなくcredentialの値を受け取り、呼び出し側でSecret値をログやStatusへ出力してはならない。
type RedfishConfig struct {
	Address            string
	SystemID           string
	Username           string
	Password           string
	CAData             []byte
	InsecureSkipVerify bool
}

// RedfishはRedfish Service RootとComputerSystem Reset actionだけを使う電源backendである。OS停止はTalosへ委譲し、PowerOffは明示的なGracefulShutdown actionとしてのみ提供する。
type Redfish struct {
	baseURL  *url.URL
	systemID string
	username string
	password string
	client   *http.Client
}

var (
	_ PowerOn            = (*Redfish)(nil)
	_ PowerOff           = (*Redfish)(nil)
	_ PowerStateObserver = (*Redfish)(nil)
)

type redfishLink struct {
	ID string `json:"@odata.id"`
}

type redfishServiceRoot struct {
	Systems redfishLink `json:"Systems"`
}

type redfishSystemCollection struct {
	Members []redfishLink `json:"Members"`
}

type redfishSystem struct {
	PowerState string                   `json:"PowerState"`
	Actions    map[string]redfishAction `json:"Actions"`
}

type redfishAction struct {
	Target string `json:"target"`
}

// NewRedfishはHTTPS endpoint、credential、TLS設定を検証してRedfish backendを構築する。
func NewRedfish(config RedfishConfig) (*Redfish, error) {
	return newRedfish(config, nil)
}

func newRedfish(config RedfishConfig, httpClient *http.Client) (*Redfish, error) {
	parsedEndpoint, err := endpoint.ParseHTTPSURL(config.Address)
	if err != nil {
		return nil, fmt.Errorf("validate Redfish address: %w", err)
	}
	baseURL, err := url.Parse(parsedEndpoint.String())
	if err != nil {
		return nil, fmt.Errorf("parse Redfish address: %w", err)
	}
	if baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, errors.New("redfish address must not contain a query or fragment")
	}
	if !strings.HasSuffix(baseURL.Path, "/") {
		baseURL.Path += "/"
	}
	if strings.TrimSpace(config.Username) == "" || strings.TrimSpace(config.Password) == "" {
		return nil, errors.New("redfish username and password are required")
	}

	if httpClient == nil {
		httpClient, err = newRedfishHTTPClient(config)
		if err != nil {
			return nil, err
		}
	}
	return &Redfish{
		baseURL:  baseURL,
		systemID: strings.TrimSpace(config.SystemID),
		username: config.Username,
		password: config.Password,
		client:   httpClient,
	}, nil
}

func newRedfishHTTPClient(config RedfishConfig) (*http.Client, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default HTTP transport is not a TCP transport")
	}
	transport = transport.Clone()
	rootCAs, err := redfishRootCAs(config.CAData)
	if err != nil {
		return nil, err
	}
	transport.TLSClientConfig = &tls.Config{
		MinVersion:         tls.VersionTLS12,
		RootCAs:            rootCAs,
		InsecureSkipVerify: config.InsecureSkipVerify, //nolint:gosec // Hostごとの明示的な設定としてTLS検証無効化を許可する。
	}
	return &http.Client{Transport: transport, Timeout: redfishRequestTimeout}, nil
}

func redfishRootCAs(data []byte) (*x509.CertPool, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(data) {
		return nil, errors.New("redfish CA data does not contain a valid PEM certificate")
	}
	return rootCAs, nil
}

// PowerOnはComputerSystemの電源状態を確認し、停止中の場合だけRedfish標準のOn actionを要求する。
func (r *Redfish) PowerOn(ctx context.Context) error {
	systemURL, system, err := r.system(ctx)
	if err != nil {
		return err
	}
	switch strings.TrimSpace(system.PowerState) {
	case string(PowerStateOn), "PoweringOn":
		return nil
	case string(PowerStateOff):
		return r.reset(ctx, systemURL, system, "On")
	default:
		return fmt.Errorf("redfish ComputerSystem is not safely power-onable from state %q", system.PowerState)
	}
}

// PowerOffはComputerSystemの電源状態を確認し、稼働中の場合だけGracefulShutdown actionを要求する。強制停止は自動選択しない。
func (r *Redfish) PowerOff(ctx context.Context) error {
	systemURL, system, err := r.system(ctx)
	if err != nil {
		return err
	}
	switch strings.TrimSpace(system.PowerState) {
	case string(PowerStateOff), "PoweringOff":
		return nil
	case string(PowerStateOn):
		return r.reset(ctx, systemURL, system, "GracefulShutdown")
	default:
		return fmt.Errorf("redfish ComputerSystem is not safely power-offable from state %q", system.PowerState)
	}
}

// PowerStateはRedfish ComputerSystemの電源状態を返す。未知のvendor拡張値もそのまま返すため、呼び出し側はOffとの完全一致だけを停止完了の根拠にする。
func (r *Redfish) PowerState(ctx context.Context) (PowerState, error) {
	_, system, err := r.system(ctx)
	if err != nil {
		return PowerStateUnknown, err
	}
	if strings.TrimSpace(system.PowerState) == "" {
		return PowerStateUnknown, errors.New("redfish ComputerSystem has no PowerState")
	}
	return PowerState(system.PowerState), nil
}

func (r *Redfish) system(ctx context.Context) (*url.URL, redfishSystem, error) {
	if r == nil || r.client == nil || r.baseURL == nil {
		return nil, redfishSystem{}, errors.New("redfish client is unavailable")
	}
	var serviceRoot redfishServiceRoot
	if err := r.request(ctx, http.MethodGet, r.baseURL, nil, &serviceRoot); err != nil {
		return nil, redfishSystem{}, fmt.Errorf("get Redfish service root: %w", err)
	}
	collectionURL, err := r.resolveLink(serviceRoot.Systems.ID)
	if err != nil {
		return nil, redfishSystem{}, fmt.Errorf("resolve Redfish Systems collection: %w", err)
	}
	var collection redfishSystemCollection
	if err := r.request(ctx, http.MethodGet, collectionURL, nil, &collection); err != nil {
		return nil, redfishSystem{}, fmt.Errorf("get Redfish Systems collection: %w", err)
	}
	memberURL, err := r.selectSystem(collection.Members)
	if err != nil {
		return nil, redfishSystem{}, err
	}
	var system redfishSystem
	if err := r.request(ctx, http.MethodGet, memberURL, nil, &system); err != nil {
		return nil, redfishSystem{}, fmt.Errorf("get Redfish ComputerSystem: %w", err)
	}
	return memberURL, system, nil
}

func (r *Redfish) selectSystem(members []redfishLink) (*url.URL, error) {
	if len(members) == 0 {
		return nil, errors.New("redfish Systems collection has no members")
	}
	if r.systemID == "" && len(members) != 1 {
		return nil, errors.New("redfish systemID is required when Systems collection has multiple members")
	}
	for _, member := range members {
		memberURL, err := r.resolveLink(member.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve Redfish ComputerSystem member: %w", err)
		}
		if r.systemID == "" || matchesSystemID(r.systemID, memberURL) {
			return memberURL, nil
		}
	}
	return nil, fmt.Errorf("redfish ComputerSystem %q was not found", r.systemID)
}

func matchesSystemID(systemID string, memberURL *url.URL) bool {
	trimmedPath := strings.TrimSuffix(memberURL.Path, "/")
	return systemID == memberURL.String() || systemID == trimmedPath || systemID == strings.TrimPrefix(trimmedPath, "/") || systemID == pathBase(trimmedPath)
}

func pathBase(value string) string {
	if _, base, found := strings.CutLast(value, "/"); found {
		return base
	}
	return value
}

func (r *Redfish) reset(ctx context.Context, systemURL *url.URL, system redfishSystem, resetType string) error {
	action, ok := system.Actions["#ComputerSystem.Reset"]
	if !ok {
		for name, candidate := range system.Actions {
			if strings.EqualFold(name, "#ComputerSystem.Reset") {
				action = candidate
				ok = true
				break
			}
		}
	}
	if !ok || strings.TrimSpace(action.Target) == "" {
		return errors.New("redfish ComputerSystem Reset action is unavailable")
	}
	actionURL, err := r.resolveLinkFrom(systemURL, action.Target)
	if err != nil {
		return fmt.Errorf("resolve Redfish ComputerSystem Reset action: %w", err)
	}
	if err := r.request(ctx, http.MethodPost, actionURL, map[string]string{"ResetType": resetType}, nil); err != nil {
		return fmt.Errorf("request Redfish ComputerSystem reset %q: %w", resetType, err)
	}
	return nil
}

func (r *Redfish) resolveLink(value string) (*url.URL, error) {
	return r.resolveLinkFrom(r.baseURL, value)
}

func (r *Redfish) resolveLinkFrom(base *url.URL, value string) (*url.URL, error) {
	if strings.TrimSpace(value) == "" {
		return nil, errors.New("redfish link is empty")
	}
	link, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("parse Redfish link: %w", err)
	}
	resolved := base.ResolveReference(link)
	if resolved.Scheme != r.baseURL.Scheme || !strings.EqualFold(resolved.Host, r.baseURL.Host) || resolved.User != nil {
		return nil, errors.New("redfish link points outside the configured endpoint")
	}
	return resolved, nil
}

func (r *Redfish) request(ctx context.Context, method string, target *url.URL, body any, responseBody any) error {
	var requestBody io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Redfish request: %w", err)
		}
		requestBody = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), requestBody)
	if err != nil {
		return fmt.Errorf("build Redfish request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.SetBasicAuth(r.username, r.password)
	response, err := r.client.Do(request)
	if err != nil {
		return fmt.Errorf("call Redfish endpoint: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		closeErr := drainRedfishResponse(response.Body)
		if closeErr != nil {
			return errors.Join(fmt.Errorf("redfish endpoint returned HTTP status %d", response.StatusCode), closeErr)
		}
		return fmt.Errorf("redfish endpoint returned HTTP status %d", response.StatusCode)
	}
	if responseBody == nil {
		return drainRedfishResponse(response.Body)
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, redfishResponseLimit+1))
	closeErr := response.Body.Close()
	if readErr != nil {
		return fmt.Errorf("read Redfish response: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close Redfish response: %w", closeErr)
	}
	if len(data) > redfishResponseLimit {
		return errors.New("redfish response exceeds the size limit")
	}
	if err := json.Unmarshal(data, responseBody); err != nil {
		return fmt.Errorf("decode Redfish response: %w", err)
	}
	return nil
}

func drainRedfishResponse(body io.ReadCloser) error {
	_, copyErr := io.Copy(io.Discard, body)
	closeErr := body.Close()
	if copyErr != nil {
		return fmt.Errorf("drain Redfish response: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close Redfish response: %w", closeErr)
	}
	return nil
}
