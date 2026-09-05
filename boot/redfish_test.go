package boot

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestRedfishPowerLifecycle(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	powerState := "Off"
	var resetTypes []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") == "" {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/":
			writeJSON(response, http.StatusOK, map[string]any{"Systems": map[string]any{"@odata.id": "/redfish/v1/Systems"}})
		case "/redfish/v1/Systems":
			writeJSON(response, http.StatusOK, map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/server-1"}}})
		case "/redfish/v1/Systems/server-1":
			mu.Lock()
			state := powerState
			mu.Unlock()
			writeJSON(response, http.StatusOK, map[string]any{
				"PowerState": state,
				"Actions":    map[string]any{"#ComputerSystem.Reset": map[string]string{"target": "/redfish/v1/Systems/server-1/Actions/ComputerSystem.Reset"}},
			})
		case "/redfish/v1/Systems/server-1/Actions/ComputerSystem.Reset":
			var body struct {
				ResetType string `json:"ResetType"`
			}
			payload, err := io.ReadAll(request.Body)
			if err != nil || json.Unmarshal(payload, &body) != nil {
				response.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			resetTypes = append(resetTypes, body.ResetType)
			switch body.ResetType {
			case "On":
				powerState = "On"
			case "GracefulShutdown":
				powerState = "Off"
			}
			mu.Unlock()
			response.WriteHeader(http.StatusNoContent)
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	backend, err := newRedfish(RedfishConfig{Address: server.URL, Username: "operator", Password: "secret"}, server.Client())
	if err != nil {
		t.Fatalf("newRedfish() error = %v", err)
	}
	if err := backend.PowerOn(t.Context()); err != nil {
		t.Fatalf("PowerOn() error = %v", err)
	}
	state, err := backend.PowerState(t.Context())
	if err != nil {
		t.Fatalf("PowerState() after PowerOn error = %v", err)
	}
	if state != PowerStateOn {
		t.Fatalf("PowerState() after PowerOn = %q, want %q", state, PowerStateOn)
	}
	if err := backend.PowerOff(t.Context()); err != nil {
		t.Fatalf("PowerOff() error = %v", err)
	}
	state, err = backend.PowerState(t.Context())
	if err != nil {
		t.Fatalf("PowerState() after PowerOff error = %v", err)
	}
	if state != PowerStateOff {
		t.Fatalf("PowerState() after PowerOff = %q, want %q", state, PowerStateOff)
	}

	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(resetTypes, []string{"On", "GracefulShutdown"}) {
		t.Fatalf("reset types = %v, want [On GracefulShutdown]", resetTypes)
	}
}

func TestRedfishRequiresExplicitSystemIDForMultipleSystems(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/":
			writeJSON(response, http.StatusOK, map[string]any{"Systems": map[string]any{"@odata.id": "/Systems"}})
		case "/Systems":
			writeJSON(response, http.StatusOK, map[string]any{"Members": []map[string]string{
				{"@odata.id": "/Systems/one"},
				{"@odata.id": "/Systems/two"},
			}})
		default:
			response.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	backend, err := newRedfish(RedfishConfig{Address: server.URL, Username: "operator", Password: "secret"}, server.Client())
	if err != nil {
		t.Fatalf("newRedfish() error = %v", err)
	}
	if _, err := backend.PowerState(t.Context()); err == nil || !strings.Contains(err.Error(), "systemID is required") {
		t.Fatalf("PowerState() error = %v, want explicit systemID error", err)
	}
}

func TestNewRedfishValidatesCredentialsAndCA(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config RedfishConfig
	}{
		{name: "invalid address", config: RedfishConfig{Address: "http://example.test", Username: "operator", Password: "secret"}},
		{name: "missing username", config: RedfishConfig{Address: "https://example.test", Password: "secret"}},
		{name: "missing password", config: RedfishConfig{Address: "https://example.test", Username: "operator"}},
		{name: "invalid CA", config: RedfishConfig{Address: "https://example.test", Username: "operator", Password: "secret", CAData: []byte("not a certificate")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewRedfish(test.config); err == nil {
				t.Fatal("NewRedfish() error = nil, want validation error")
			}
		})
	}
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	if _, err := response.Write(payload); err != nil {
		panic(err)
	}
}
