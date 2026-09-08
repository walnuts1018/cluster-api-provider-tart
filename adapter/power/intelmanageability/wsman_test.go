package intelmanageability

import (
	"crypto/md5" //nolint:gosec // testの期待値算出でも実プロトコルと同じRFC 2617 MD5を使う。
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	testUsername = "admin"
	testPassword = "s3cr3t-pass"
	testRealm    = "Digest:A4000000-0000-0000-0000-000000000000"
)

func TestWSManDigest認証(t *testing.T) {
	t.Parallel()

	var authenticatedRequests int
	handler := func(w http.ResponseWriter, r *http.Request) {
		authorization := r.Header.Get("Authorization")
		if authorization == "" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Digest realm=%q, nonce="abc123", qop="auth"`, testRealm))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !validDigestAuthorization(t, authorization, http.MethodPost, r.URL.RequestURI()) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		authenticatedRequests++
		writeEnumerateResponse(w, `<n1:PowerState xmlns:n1="`+resourceURIAssociatedPowerManagementService+`">2</n1:PowerState>`)
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)

	c := newTestClient(t, server.URL)
	instances, err := c.enumerateAll(t.Context(), resourceURIAssociatedPowerManagementService)
	if err != nil {
		t.Fatalf("enumerateAll() error = %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("enumerateAll() len = %d, want 1", len(instances))
	}
	if authenticatedRequests != 1 {
		t.Fatalf("authenticatedRequests = %d, want 1", authenticatedRequests)
	}
}

func TestWSMan認証失敗(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Digest realm=%q, nonce="abc123", qop="auth"`, testRealm))
		w.WriteHeader(http.StatusUnauthorized)
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)

	c := newTestClient(t, server.URL)
	_, err := c.enumerateAll(t.Context(), resourceURIAssociatedPowerManagementService)
	if !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatalf("enumerateAll() error = %v, want ErrAuthenticationFailed", err)
	}
}

func TestWSMan認証チャレンジなしの401(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)

	c := newTestClient(t, server.URL)
	_, err := c.enumerateAll(t.Context(), resourceURIAssociatedPowerManagementService)
	if !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatalf("enumerateAll() error = %v, want ErrAuthenticationFailed", err)
	}
}

func TestWSMan接続失敗(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, "http://127.0.0.1:1")
	_, err := c.enumerateAll(t.Context(), resourceURIAssociatedPowerManagementService)
	if !errors.Is(err, ErrConnectionFailed) {
		t.Fatalf("enumerateAll() error = %v, want ErrConnectionFailed", err)
	}
}

func TestWSMan不正なXML(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "not xml")
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)

	c := newTestClient(t, server.URL)
	_, err := c.enumerateAll(t.Context(), resourceURIAssociatedPowerManagementService)
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("enumerateAll() error = %v, want ErrProtocol", err)
	}
}

func TestWSManSOAPFault(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `<?xml version="1.0"?>
<s:Envelope xmlns:s="`+nsSOAPEnvelope+`">
  <s:Body>
    <s:Fault>
      <s:Reason><s:Text>access denied</s:Text></s:Reason>
    </s:Fault>
  </s:Body>
</s:Envelope>`)
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)

	c := newTestClient(t, server.URL)
	_, err := c.enumerateAll(t.Context(), resourceURIAssociatedPowerManagementService)
	if !errors.Is(err, ErrProtocol) || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("enumerateAll() error = %v, want ErrProtocol containing fault reason", err)
	}
}

func TestWSManHTTPステータスエラー(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)

	c := newTestClient(t, server.URL)
	_, err := c.enumerateAll(t.Context(), resourceURIAssociatedPowerManagementService)
	if !errors.Is(err, ErrProtocol) || !strings.Contains(err.Error(), "502") {
		t.Fatalf("enumerateAll() error = %v, want ErrProtocol containing 502", err)
	}
}

func TestWSManInvoke(t *testing.T) {
	t.Parallel()

	var receivedMethod string
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if strings.Contains(string(body), "RequestPowerStateChange_INPUT") {
			receivedMethod = "RequestPowerStateChange"
		}
		_, _ = io.WriteString(w, `<?xml version="1.0"?>
<s:Envelope xmlns:s="`+nsSOAPEnvelope+`">
  <s:Body>
    <n:RequestPowerStateChange_OUTPUT xmlns:n="`+resourceURIPowerManagementService+`">
      <n:ReturnValue>0</n:ReturnValue>
    </n:RequestPowerStateChange_OUTPUT>
  </s:Body>
</s:Envelope>`)
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)

	c := newTestClient(t, server.URL)
	body, err := c.invoke(t.Context(), resourceURIPowerManagementService, "RequestPowerStateChange", []invokeParam{
		{name: "PowerState", value: cimPowerStateOn},
	})
	if err != nil {
		t.Fatalf("invoke() error = %v", err)
	}
	if receivedMethod != "RequestPowerStateChange" {
		t.Fatalf("server did not observe RequestPowerStateChange_INPUT in request body")
	}
	if returnValue := body.find("ReturnValue"); returnValue == nil || returnValue.Content != "0" {
		t.Fatalf("ReturnValue = %v, want 0", returnValue)
	}
}

func TestWSManPull(t *testing.T) {
	t.Parallel()

	requestCount := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		requestCount++
		switch {
		case strings.Contains(string(body), "<Enumerate"):
			_, _ = io.WriteString(w, `<?xml version="1.0"?>
<s:Envelope xmlns:s="`+nsSOAPEnvelope+`">
  <s:Body>
    <wsen:EnumerateResponse xmlns:wsen="`+nsWSEnumerate+`">
      <wsen:EnumerationContext>ctx-1</wsen:EnumerationContext>
    </wsen:EnumerateResponse>
  </s:Body>
</s:Envelope>`)
		case strings.Contains(string(body), "<Pull"):
			_, _ = io.WriteString(w, `<?xml version="1.0"?>
<s:Envelope xmlns:s="`+nsSOAPEnvelope+`">
  <s:Body>
    <wsen:PullResponse xmlns:wsen="`+nsWSEnumerate+`">
      <wsman:Items xmlns:wsman="`+nsWSManagement+`">
        <n1:CIM_AssociatedPowerManagementService xmlns:n1="`+resourceURIAssociatedPowerManagementService+`">
          <n1:PowerState>8</n1:PowerState>
        </n1:CIM_AssociatedPowerManagementService>
      </wsman:Items>
    </wsen:PullResponse>
  </s:Body>
</s:Envelope>`)
		default:
			t.Fatalf("unexpected request body: %s", body)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)

	c := newTestClient(t, server.URL)
	instances, err := c.enumerateAll(t.Context(), resourceURIAssociatedPowerManagementService)
	if err != nil {
		t.Fatalf("enumerateAll() error = %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("requestCount = %d, want 2 (Enumerate + Pull)", requestCount)
	}
	if len(instances) != 1 {
		t.Fatalf("enumerateAll() len = %d, want 1", len(instances))
	}
	if node := instances[0].find("PowerState"); node == nil || node.Content != "8" {
		t.Fatalf("PowerState = %v, want 8", node)
	}
}

func newTestClient(t *testing.T, address string) *client {
	t.Helper()
	c, err := newClient(clientConfig{
		endpoint: address,
		username: testUsername,
		password: testPassword,
	})
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}
	return c
}

func writeEnumerateResponse(w http.ResponseWriter, instanceXML string) {
	_, _ = io.WriteString(w, `<?xml version="1.0"?>
<s:Envelope xmlns:s="`+nsSOAPEnvelope+`">
  <s:Body>
    <wsen:EnumerateResponse xmlns:wsen="`+nsWSEnumerate+`">
      <wsman:Items xmlns:wsman="`+nsWSManagement+`">
        <n1:CIM_AssociatedPowerManagementService xmlns:n1="`+resourceURIAssociatedPowerManagementService+`">
          `+instanceXML+`
        </n1:CIM_AssociatedPowerManagementService>
      </wsman:Items>
    </wsen:EnumerateResponse>
  </s:Body>
</s:Envelope>`)
}

// validDigestAuthorizationはtest serverが受け取ったAuthorizationヘッダを、client側と同じRFC 2617の計算式で再計算し一致するか検証する。
func validDigestAuthorization(t *testing.T, header, method, uri string) bool {
	t.Helper()
	if !strings.HasPrefix(header, "Digest ") {
		return false
	}
	params := parseDigestParams(strings.TrimPrefix(header, "Digest "))
	if params["username"] != testUsername {
		return false
	}
	ha1 := md5HexForTest(testUsername + ":" + params["realm"] + ":" + testPassword)
	ha2 := md5HexForTest(method + ":" + uri)
	expected := md5HexForTest(strings.Join([]string{ha1, params["nonce"], params["nc"], params["cnonce"], "auth", ha2}, ":"))
	return params["response"] == expected
}

func md5HexForTest(value string) string {
	sum := md5.Sum([]byte(value)) //nolint:gosec // test用の期待値算出。
	return hex.EncodeToString(sum[:])
}
