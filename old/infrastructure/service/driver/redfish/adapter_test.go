package redfish

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	capabilitydomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/capability"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/driver"
)

const redfishBootTargetUefiHTTP = "UefiHttp"

func TestAdapterDiscoversCapabilitiesWithSessionAuthentication(t *testing.T) {
	t.Parallel()

	fixture := &redfishFixture{}
	adapter := NewWithTransport(roundTripFunc(fixture.roundTrip))
	target := testTarget(t)
	got, err := adapter.DiscoverCapabilities(t.Context(), driverdomain.Redfish, target, applicationdriver.Invocation{})
	if err != nil {
		t.Fatalf("DiscoverCapabilities() error = %v", err)
	}

	want, err := capabilitydomain.NewSet(
		capabilitydomain.PowerOn,
		capabilitydomain.PowerOff,
		capabilitydomain.ObservePowerState,
		capabilitydomain.SetNextBoot,
		capabilitydomain.VirtualMedia,
	)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	if !got.ContainsAll(want) {
		t.Fatalf("capabilities = %v, want %v", got.Values(), want.Values())
	}
	if fixture.usedBasicAuth {
		t.Fatal("usedBasicAuth = true, want false")
	}
}

func TestAdapterFallsBackToBasicAuthenticationOnlyWhenSessionIsUnsupported(t *testing.T) {
	t.Parallel()

	fixture := &redfishFixture{sessionUnsupported: true}
	adapter := NewWithTransport(roundTripFunc(fixture.roundTrip))
	target := testTarget(t)
	if _, err := adapter.DiscoverCapabilities(t.Context(), driverdomain.Redfish, target, applicationdriver.Invocation{}); err != nil {
		t.Fatalf("DiscoverCapabilities() error = %v", err)
	}
	if !fixture.usedBasicAuth {
		t.Fatal("usedBasicAuth = false, want true")
	}
}

func TestAdapterRejectsAuthenticationFailureWithoutBasicFallback(t *testing.T) {
	t.Parallel()

	fixture := &redfishFixture{sessionAuthFails: true}
	adapter := NewWithTransport(roundTripFunc(fixture.roundTrip))
	target := testTarget(t)
	if _, err := adapter.DiscoverCapabilities(t.Context(), driverdomain.Redfish, target, applicationdriver.Invocation{}); !driverdomain.IsErrorKind(err, driverdomain.ErrorAuthenticationFailed) {
		t.Fatalf("DiscoverCapabilities() error = %v, want AuthenticationFailed", err)
	}
	if fixture.usedBasicAuth {
		t.Fatal("usedBasicAuth = true, want false")
	}
}

func TestAdapterRejectsTLSVerificationMismatch(t *testing.T) {
	t.Parallel()

	adapter := New()
	access, err := driverdomain.NewRedfishAccess(
		"https://bmc.example.test",
		"admin",
		"secret",
		nil,
		[]string{"sha256:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB="},
	)
	if err != nil {
		t.Fatalf("NewRedfishAccess() error = %v", err)
	}
	client, err := adapter.newHTTPClient(access)
	if err != nil {
		t.Fatalf("newHTTPClient() error = %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.VerifyConnection == nil {
		t.Fatal("VerifyConnection = nil, want configured pin verifier")
	}
	certificate := testCertificate(t)
	err = transport.TLSClientConfig.VerifyConnection(tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{certificate},
	})
	if !driverdomain.IsErrorKind(err, driverdomain.ErrorTLSVerificationFailed) {
		t.Fatalf("VerifyConnection() error = %v, want TLSVerificationFailed", err)
	}
}

func TestAdapterMountIsIdempotentForSameOperation(t *testing.T) {
	t.Parallel()

	fixture := &redfishFixture{
		virtualMediaInserted: true,
		virtualMediaImage:    "https://controller.example.test/agent.iso",
		virtualMediaOpID:     "f4353748-c9ea-41c6-b321-94197b64330e",
	}
	adapter := NewWithTransport(roundTripFunc(fixture.roundTrip))
	target := testTarget(t)
	artifact, err := driverdomain.NewArtifact("https://controller.example.test/agent.iso")
	if err != nil {
		t.Fatalf("NewArtifact() error = %v", err)
	}
	operationID, err := operationdomain.ParseID("f4353748-c9ea-41c6-b321-94197b64330e")
	if err != nil {
		t.Fatalf("ParseID() error = %v", err)
	}
	if err := adapter.Mount(t.Context(), target, artifact, operationID); err != nil {
		t.Fatalf("Mount() error = %v", err)
	}
	if fixture.insertCount != 0 {
		t.Fatalf("insertCount = %d, want 0", fixture.insertCount)
	}
}

func TestAdapterMountRejectsConflictingMedia(t *testing.T) {
	t.Parallel()

	fixture := &redfishFixture{
		virtualMediaInserted: true,
		virtualMediaImage:    "https://controller.example.test/other.iso",
		virtualMediaOpID:     "11111111-1111-1111-1111-111111111111",
	}
	adapter := NewWithTransport(roundTripFunc(fixture.roundTrip))
	target := testTarget(t)
	artifact, err := driverdomain.NewArtifact("https://controller.example.test/agent.iso")
	if err != nil {
		t.Fatalf("NewArtifact() error = %v", err)
	}
	operationID, err := operationdomain.ParseID("f4353748-c9ea-41c6-b321-94197b64330e")
	if err != nil {
		t.Fatalf("ParseID() error = %v", err)
	}
	if err := adapter.Mount(t.Context(), target, artifact, operationID); !driverdomain.IsErrorKind(err, driverdomain.ErrorConflict) {
		t.Fatalf("Mount() error = %v, want Conflict", err)
	}
}

func TestAdapterSetsOneTimeBootOverride(t *testing.T) {
	t.Parallel()

	fixture := &redfishFixture{}
	adapter := NewWithTransport(roundTripFunc(fixture.roundTrip))
	target := testTarget(t)
	operationID, err := operationdomain.ParseID("f4353748-c9ea-41c6-b321-94197b64330e")
	if err != nil {
		t.Fatalf("ParseID() error = %v", err)
	}
	if err := adapter.SetNextBoot(t.Context(), target, driverdomain.BootTargetHTTP, operationID); err != nil {
		t.Fatalf("SetNextBoot() error = %v", err)
	}
	boot, ok := fixture.lastBootPatch["Boot"].(map[string]any)
	if !ok {
		t.Fatalf("Boot patch = %T, want map[string]any", fixture.lastBootPatch["Boot"])
	}
	if got := boot["BootSourceOverrideEnabled"]; got != "Once" {
		t.Fatalf("BootSourceOverrideEnabled = %v, want Once", got)
	}
	if got := boot["BootSourceOverrideTarget"]; got != redfishBootTargetUefiHTTP {
		t.Fatalf("BootSourceOverrideTarget = %v, want %s", got, redfishBootTargetUefiHTTP)
	}
}

func TestAdapterObservesBootState(t *testing.T) {
	t.Parallel()

	fixture := &redfishFixture{
		bootOverrideEnabled:  "Once",
		bootOverrideTarget:   "Cd",
		virtualMediaInserted: true,
		virtualMediaImage:    "https://controller.example.test/agent.iso",
		virtualMediaOpID:     "f4353748-c9ea-41c6-b321-94197b64330e",
	}
	adapter := NewWithTransport(roundTripFunc(fixture.roundTrip))
	target := testTarget(t)

	state, err := adapter.ObserveBootState(t.Context(), target)
	if err != nil {
		t.Fatalf("ObserveBootState() error = %v", err)
	}
	if !state.OverrideEnabled {
		t.Fatal("OverrideEnabled = false, want true")
	}
	if state.OverrideTarget != driverdomain.BootTargetVirtualMedia {
		t.Fatalf("OverrideTarget = %q, want VirtualMedia", state.OverrideTarget)
	}
	if !state.MediaInserted {
		t.Fatal("MediaInserted = false, want true")
	}
	if state.MediaImage != "https://controller.example.test/agent.iso" {
		t.Fatalf("MediaImage = %q, want mounted image", state.MediaImage)
	}
	if state.MediaOperation != "f4353748-c9ea-41c6-b321-94197b64330e" {
		t.Fatalf("MediaOperation = %q, want operation ID", state.MediaOperation)
	}
}

type redfishFixture struct {
	sessionUnsupported   bool
	sessionAuthFails     bool
	bootOverrideEnabled  string
	bootOverrideTarget   string
	virtualMediaInserted bool
	virtualMediaImage    string
	virtualMediaOpID     string
	usedBasicAuth        bool
	insertCount          int
	lastBootPatch        map[string]any
}

func (fixture *redfishFixture) roundTrip(request *http.Request) (*http.Response, error) {
	switch request.URL.Path {
	case "/redfish/v1/":
		return fixture.jsonResponse(map[string]any{
			"Systems":        map[string]string{"@odata.id": "/redfish/v1/Systems"},
			"Managers":       map[string]string{"@odata.id": "/redfish/v1/Managers"},
			"SessionService": map[string]string{"@odata.id": "/redfish/v1/SessionService"},
		}), nil
	case "/redfish/v1/SessionService/Sessions":
		if fixture.sessionUnsupported {
			return fixture.emptyResponse(http.StatusNotFound), nil
		}
		if fixture.sessionAuthFails {
			return fixture.emptyResponse(http.StatusUnauthorized), nil
		}
		response := fixture.emptyResponse(http.StatusCreated)
		response.Header.Set("X-Auth-Token", "session-token")
		return response, nil
	}
	if !fixture.authenticated(request) {
		return fixture.emptyResponse(http.StatusUnauthorized), nil
	}
	switch request.URL.Path {
	case "/redfish/v1/Systems":
		return fixture.jsonResponse(map[string]any{
			"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/1"}},
		}), nil
	case "/redfish/v1/Systems/1":
		if request.Method == http.MethodPatch {
			defer request.Body.Close()
			if err := json.NewDecoder(request.Body).Decode(&fixture.lastBootPatch); err != nil {
				return fixture.emptyResponse(http.StatusBadRequest), nil
			}
			return fixture.emptyResponse(http.StatusNoContent), nil
		}
		return fixture.jsonResponse(map[string]any{
			"PowerState": "On",
			"Boot": map[string]any{
				"BootSourceOverrideTarget@Redfish.AllowableValues": []string{"Pxe", redfishBootTargetUefiHTTP, "Cd"},
				"BootSourceOverrideEnabled":                        fixture.bootOverrideEnabled,
				"BootSourceOverrideTarget":                         fixture.bootOverrideTarget,
			},
			"Actions": map[string]any{
				"#ComputerSystem.Reset": map[string]string{
					"target": "/redfish/v1/Systems/1/Actions/ComputerSystem.Reset",
				},
			},
		}), nil
	case "/redfish/v1/Systems/1/Actions/ComputerSystem.Reset":
		return fixture.emptyResponse(http.StatusNoContent), nil
	case "/redfish/v1/Managers":
		return fixture.jsonResponse(map[string]any{
			"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/1"}},
		}), nil
	case "/redfish/v1/Managers/1":
		return fixture.jsonResponse(map[string]any{
			"VirtualMedia": map[string]string{"@odata.id": "/redfish/v1/Managers/1/VirtualMedia"},
		}), nil
	case "/redfish/v1/Managers/1/VirtualMedia":
		return fixture.jsonResponse(map[string]any{
			"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/1/VirtualMedia/CD1"}},
		}), nil
	case "/redfish/v1/Managers/1/VirtualMedia/CD1":
		return fixture.jsonResponse(map[string]any{
			"MediaTypes": []string{"CD"},
			"Image":      fixture.virtualMediaImage,
			"Inserted":   fixture.virtualMediaInserted,
			"Oem": map[string]any{
				"TART": map[string]string{"OperationID": fixture.virtualMediaOpID},
			},
			"Actions": map[string]any{
				"#VirtualMedia.InsertMedia": map[string]string{
					"target": "/redfish/v1/Managers/1/VirtualMedia/CD1/Actions/VirtualMedia.InsertMedia",
				},
				"#VirtualMedia.EjectMedia": map[string]string{
					"target": "/redfish/v1/Managers/1/VirtualMedia/CD1/Actions/VirtualMedia.EjectMedia",
				},
			},
		}), nil
	case "/redfish/v1/Managers/1/VirtualMedia/CD1/Actions/VirtualMedia.InsertMedia":
		fixture.insertCount++
		return fixture.emptyResponse(http.StatusNoContent), nil
	case "/redfish/v1/Managers/1/VirtualMedia/CD1/Actions/VirtualMedia.EjectMedia":
		return fixture.emptyResponse(http.StatusNoContent), nil
	default:
		return fixture.emptyResponse(http.StatusNotFound), nil
	}
}

func (fixture *redfishFixture) authenticated(request *http.Request) bool {
	if request.Header.Get("X-Auth-Token") == "session-token" {
		return true
	}
	username, password, ok := request.BasicAuth()
	if ok && username == "admin" && password == "secret" {
		fixture.usedBasicAuth = true
		return true
	}
	return false
}

func (fixture *redfishFixture) jsonResponse(body any) *http.Response {
	payload, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(payload)),
	}
}

func (fixture *redfishFixture) emptyResponse(statusCode int) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func testTarget(t *testing.T) driverdomain.HostTarget {
	t.Helper()
	address, err := driverdomain.ParseMACAddress("00:00:5e:00:53:02")
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}
	access, err := driverdomain.NewRedfishAccess("https://bmc.example.test", "admin", "secret", nil, nil)
	if err != nil {
		t.Fatalf("NewRedfishAccess() error = %v", err)
	}
	return driverdomain.NewHostTarget(address).WithRedfishAccess(access)
}

func testCertificate(t *testing.T) *x509.Certificate {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "bmc.example.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"bmc.example.test"},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ParseCertificate() error = %v", err)
	}
	_ = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return certificate
}
