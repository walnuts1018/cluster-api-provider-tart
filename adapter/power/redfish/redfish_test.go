package redfish

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const (
	testRedfishUsername = "operator"
	testRedfishPassword = "secret"
)

func TestRedfishURL検証(t *testing.T) {
	tests := []struct {
		name        string
		address     string
		username    string
		password    string
		wantPath    string
		wantErrorIn string
	}{
		{name: "末尾スラッシュを補う", address: "https://bmc.test.walnuts.dev/redfish/v1", username: testRedfishUsername, password: testRedfishPassword, wantPath: "/redfish/v1/"},
		{name: "queryを拒否", address: "https://bmc.test.walnuts.dev/redfish/v1?next=admin", username: testRedfishUsername, password: testRedfishPassword, wantErrorIn: "query or fragment"},
		{name: "fragmentを拒否", address: "https://bmc.test.walnuts.dev/redfish/v1#systems", username: testRedfishUsername, password: testRedfishPassword, wantErrorIn: "query or fragment"},
		{name: "HTTPS以外を拒否", address: "http://bmc.test.walnuts.dev/redfish/v1", username: testRedfishUsername, password: testRedfishPassword, wantErrorIn: "validate Redfish address"},
		{name: "空の認証情報を拒否", address: "https://bmc.test.walnuts.dev/redfish/v1", username: " ", password: testRedfishPassword, wantErrorIn: "username and password"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend, err := newBackend(Config{
				Address:  test.address,
				Username: test.username,
				Password: test.password,
			}, &http.Client{})
			if test.wantErrorIn != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErrorIn) {
					t.Fatalf("newBackend() error = %v, want substring %q", err, test.wantErrorIn)
				}
				return
			}
			if err != nil {
				t.Fatalf("newBackend() error = %v", err)
			}
			if backend.baseURL.Path != test.wantPath {
				t.Fatalf("baseURL.Path = %q, want %q", backend.baseURL.Path, test.wantPath)
			}
		})
	}
}

func TestRedfishURL境界(t *testing.T) {
	baseURL, err := url.Parse("https://bmc.test.walnuts.dev/redfish/v1/")
	if err != nil {
		t.Fatalf("テストURLの解析に失敗: %v", err)
	}
	backend := &Backend{baseURL: baseURL}

	tests := []struct {
		name      string
		link      string
		wantPath  string
		wantError bool
	}{
		{name: "相対リンク", link: "Systems/node-a", wantPath: "/redfish/v1/Systems/node-a"},
		{name: "同一endpointの絶対パス", link: "/redfish/v1/Systems/node-a", wantPath: "/redfish/v1/Systems/node-a"},
		{name: "endpoint外への相対移動", link: "../../admin", wantError: true},
		{name: "endpoint外の絶対パス", link: "/admin", wantError: true},
		{name: "別host", link: "https://other.test.walnuts.dev/redfish/v1/Systems/node-a", wantError: true},
		{name: "userinfo付きURL", link: "https://operator@bmc.test.walnuts.dev/redfish/v1/Systems/node-a", wantError: true},
		{name: "空のリンク", link: " ", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolved, err := backend.resolveLink(test.link)
			if test.wantError {
				if err == nil {
					t.Fatalf("resolveLink(%q) succeeded with %s", test.link, resolved)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveLink(%q) error = %v", test.link, err)
			}
			if resolved.Path != test.wantPath {
				t.Fatalf("resolveLink(%q).Path = %q, want %q", test.link, resolved.Path, test.wantPath)
			}
		})
	}
}

func TestRedfishSystem選択(t *testing.T) {
	baseURL, err := url.Parse("https://bmc.test.walnuts.dev/redfish/v1/")
	if err != nil {
		t.Fatalf("テストURLの解析に失敗: %v", err)
	}
	members := []redfishLink{
		{ID: "/redfish/v1/Systems/node-a"},
		{ID: "/redfish/v1/Systems/node-b"},
	}

	tests := []struct {
		name      string
		systemID  string
		members   []redfishLink
		wantPath  string
		wantError string
	}{
		{name: "単一memberを自動選択", members: members[:1], wantPath: "/redfish/v1/Systems/node-a"},
		{name: "複数memberで未指定を拒否", members: members, wantError: "systemID is required"},
		{name: "basenameで選択", systemID: "node-b", members: members, wantPath: "/redfish/v1/Systems/node-b"},
		{name: "相対pathで選択", systemID: "redfish/v1/Systems/node-a", members: members, wantPath: "/redfish/v1/Systems/node-a"},
		{name: "絶対URLで選択", systemID: "https://bmc.test.walnuts.dev/redfish/v1/Systems/node-b", members: members, wantPath: "/redfish/v1/Systems/node-b"},
		{name: "存在しないsystemを拒否", systemID: "node-c", members: members, wantError: "was not found"},
		{name: "memberなしを拒否", systemID: "node-a", wantError: "has no members"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &Backend{baseURL: baseURL, systemID: test.systemID}
			selected, err := backend.selectSystem(test.members)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("selectSystem() error = %v, want substring %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("selectSystem() error = %v", err)
			}
			if selected.Path != test.wantPath {
				t.Fatalf("選択されたsystemのpath = %q, want %q", selected.Path, test.wantPath)
			}
		})
	}
}

func TestRedfish電源操作の安全な状態遷移(t *testing.T) {
	tests := []struct {
		name      string
		state     string
		powerOn   bool
		wantReset string
		wantError bool
	}{
		{name: "OnのPowerOnは成功", state: "On", powerOn: true},
		{name: "PoweringOnのPowerOnは成功", state: "PoweringOn", powerOn: true},
		{name: "OffのPowerOnはOnを要求", state: "Off", powerOn: true, wantReset: "On"},
		{name: "未知状態のPowerOnは停止", state: "Unknown", powerOn: true, wantError: true},
		{name: "OffのPowerOffは成功", state: "Off"},
		{name: "PoweringOffのPowerOffは成功", state: "PoweringOff"},
		{name: "OnのPowerOffはGracefulShutdownを要求", state: "On", wantReset: "GracefulShutdown"},
		{name: "未知状態のPowerOffは停止", state: "Unknown", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var resetType string
			serverHandler := func(w http.ResponseWriter, r *http.Request) {
				if username, password, ok := r.BasicAuth(); !ok || username != testRedfishUsername || password != testRedfishPassword {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				switch r.URL.Path {
				case "/redfish/v1/":
					_, _ = io.WriteString(w, `{"Systems":{"@odata.id":"/redfish/v1/Systems"}}`)
				case "/redfish/v1/Systems":
					_, _ = io.WriteString(w, `{"Members":[{"@odata.id":"/redfish/v1/Systems/node-a"}]}`)
				case "/redfish/v1/Systems/node-a":
					_, _ = io.WriteString(w, `{"PowerState":"`+test.state+`","Actions":{"#ComputerSystem.Reset":{"target":"/redfish/v1/Systems/node-a/Actions/ComputerSystem.Reset"}}}`)
				case "/redfish/v1/Systems/node-a/Actions/ComputerSystem.Reset":
					if r.Method != http.MethodPost {
						http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
						return
					}
					payload, err := io.ReadAll(r.Body)
					if err != nil {
						http.Error(w, "bad request", http.StatusBadRequest)
						return
					}
					var body map[string]string
					if err := json.Unmarshal(payload, &body); err != nil {
						http.Error(w, "bad request", http.StatusBadRequest)
						return
					}
					resetType = body["ResetType"]
				default:
					http.NotFound(w, r)
				}
			}
			backend := newRedfishTestBackend(t, serverHandler, "")

			var err error
			if test.powerOn {
				err = backend.PowerOn(t.Context())
			} else {
				err = backend.PowerOff(t.Context())
			}
			if (err != nil) != test.wantError {
				t.Fatalf("電源操作のerror = %v, wantError = %t", err, test.wantError)
			}
			if resetType != test.wantReset {
				t.Fatalf("ResetType = %q, want %q", resetType, test.wantReset)
			}
		})
	}
}

func TestRedfish認証(t *testing.T) {
	authenticated := true
	handler := func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != testRedfishUsername || password != testRedfishPassword {
			authenticated = false
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/redfish/v1/":
			_, _ = io.WriteString(w, `{"Systems":{"@odata.id":"/redfish/v1/Systems"}}`)
		case "/redfish/v1/Systems":
			_, _ = io.WriteString(w, `{"Members":[{"@odata.id":"/redfish/v1/Systems/node-a"}]}`)
		case "/redfish/v1/Systems/node-a":
			_, _ = io.WriteString(w, `{"PowerState":"On"}`)
		default:
			http.NotFound(w, r)
		}
	}
	backend := newRedfishTestBackend(t, handler, "")

	state, err := backend.PowerState(t.Context())
	if err != nil || state != PowerStateOn {
		t.Fatalf("PowerState() = %q, %v, want %q, nil", state, err, PowerStateOn)
	}
	if !authenticated {
		t.Fatal("リクエストに正しいBasic認証情報が設定されていない")
	}
}

func TestRedfishHTTPStatusエラー(t *testing.T) {
	handler := func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream failure", http.StatusBadGateway)
	}
	backend := newRedfishTestBackend(t, handler, "")

	_, err := backend.PowerState(t.Context())
	if err == nil || !strings.Contains(err.Error(), "HTTP status 502") {
		t.Fatalf("PowerState() error = %v, want HTTP status 502", err)
	}
}

func TestRedfishResponseサイズ制限(t *testing.T) {
	response := strings.Repeat("x", redfishResponseLimit+1)
	handler := func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, response)
	}
	backend := newRedfishTestBackend(t, handler, "")

	_, err := backend.PowerState(t.Context())
	if err == nil || !strings.Contains(err.Error(), "exceeds the size limit") {
		t.Fatalf("PowerState() error = %v, want response size limit error", err)
	}
}

func newRedfishTestBackend(t *testing.T, handler http.HandlerFunc, systemID string) *Backend {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	backend, err := newBackend(Config{
		Address:  "https://" + server.Listener.Addr().String() + "/redfish/v1",
		SystemID: systemID,
		Username: testRedfishUsername,
		Password: testRedfishPassword,
	}, server.Client())
	if err != nil {
		t.Fatalf("newBackend() error = %v", err)
	}
	return backend
}
