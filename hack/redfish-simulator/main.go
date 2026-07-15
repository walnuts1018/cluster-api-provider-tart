// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const (
	username = "admin"
	password = "secret"
)

type simulator struct {
	mu                sync.Mutex
	sessionSupported  bool
	sessionAttempts   int
	sessionCreated    int
	basicAuthRequests int
	tokenAuthRequests int
	bootPatchCount    int
	bootConsumeCount  int
	insertCount       int
	ejectCount        int
	normalBootOrder   []string
	bootHistory       []string
	bootEnabled       string
	bootTarget        string
	mediaInserted     bool
	mediaImage        string
	mediaOperationID  string
}

type readyPayload struct {
	Endpoint    string `json:"endpoint"`
	CABundlePEM string `json:"caBundlePEM"`
}

type debugState struct {
	SessionSupported  bool     `json:"sessionSupported"`
	SessionAttempts   int      `json:"sessionAttempts"`
	SessionCreated    int      `json:"sessionCreated"`
	BasicAuthRequests int      `json:"basicAuthRequests"`
	TokenAuthRequests int      `json:"tokenAuthRequests"`
	BootPatchCount    int      `json:"bootPatchCount"`
	BootConsumeCount  int      `json:"bootConsumeCount"`
	InsertCount       int      `json:"insertCount"`
	EjectCount        int      `json:"ejectCount"`
	NormalBootOrder   []string `json:"normalBootOrder"`
	BootHistory       []string `json:"bootHistory"`
	BootEnabled       string   `json:"bootEnabled"`
	BootTarget        string   `json:"bootTarget"`
	MediaInserted     bool     `json:"mediaInserted"`
	MediaImage        string   `json:"mediaImage"`
	MediaOperationID  string   `json:"mediaOperationID"`
}

type bootPatchRequest struct {
	Boot struct {
		Enabled string `json:"BootSourceOverrideEnabled"`
		Target  string `json:"BootSourceOverrideTarget"`
	} `json:"Boot"`
}

type resetRequest struct {
	ResetType string `json:"ResetType"`
}

type insertMediaRequest struct {
	Image string `json:"Image"`
	Oem   struct {
		TART struct {
			OperationID string `json:"OperationID"`
		} `json:"TART"`
	} `json:"Oem"`
}

func main() {
	mode := flag.String("session-mode", "supported", "supported or unsupported")
	readyFile := flag.String("ready-file", "", "path to write endpoint and CA bundle")
	flag.Parse()

	if *readyFile == "" {
		log.Fatal("ready-file is required")
	}

	state := &simulator{
		sessionSupported: *mode == "supported",
		normalBootOrder:  []string{"Pxe", "Hdd"},
	}

	certificatePEM, keyPEM, err := generateCertificate()
	if err != nil {
		log.Fatalf("generate certificate: %v", err)
	}
	serverCertificate, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil {
		log.Fatalf("load key pair: %v", err)
	}

	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{serverCertificate},
	})
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer func() {
		if closeErr := listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			log.Printf("close listener: %v", closeErr)
		}
	}()

	server := &http.Server{
		Handler:           handler(state),
		ReadHeaderTimeout: 5 * time.Second,
	}

	if err := writeReadyFile(*readyFile, readyPayload{
		Endpoint:    "https://" + listener.Addr().String(),
		CABundlePEM: string(certificatePEM),
	}); err != nil {
		log.Fatalf("write ready file: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve: %v", err)
		}
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
}

func handler(state *simulator) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/state", state.handleDebugState)
	mux.HandleFunc("/redfish/v1/", state.handleRoot)
	mux.HandleFunc("/redfish/v1/SessionService/Sessions", state.handleSessions)
	mux.HandleFunc("/redfish/v1/Systems", state.handleSystems)
	mux.HandleFunc("/redfish/v1/Systems/1", state.handleSystem)
	mux.HandleFunc("/redfish/v1/Systems/1/Actions/ComputerSystem.Reset", state.handleReset)
	mux.HandleFunc("/redfish/v1/Managers", state.handleManagers)
	mux.HandleFunc("/redfish/v1/Managers/1", state.handleManager)
	mux.HandleFunc("/redfish/v1/Managers/1/VirtualMedia", state.handleVirtualMediaCollection)
	mux.HandleFunc("/redfish/v1/Managers/1/VirtualMedia/CD1", state.handleVirtualMedia)
	mux.HandleFunc("/redfish/v1/Managers/1/VirtualMedia/CD1/Actions/VirtualMedia.InsertMedia", state.handleInsertMedia)
	mux.HandleFunc("/redfish/v1/Managers/1/VirtualMedia/CD1/Actions/VirtualMedia.EjectMedia", state.handleEjectMedia)
	return mux
}

func (state *simulator) handleDebugState(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, state.snapshot())
}

func (state *simulator) handleRoot(response http.ResponseWriter, _ *http.Request) {
	payload := map[string]any{
		"Systems":  map[string]string{"@odata.id": "/redfish/v1/Systems"},
		"Managers": map[string]string{"@odata.id": "/redfish/v1/Managers"},
	}
	if state.snapshot().SessionSupported {
		payload["SessionService"] = map[string]string{"@odata.id": "/redfish/v1/SessionService"}
	}
	writeJSON(response, payload)
}

func (state *simulator) handleSessions(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.NotFound(response, request)
		return
	}

	state.mu.Lock()
	state.sessionAttempts++
	enabled := state.sessionSupported
	state.mu.Unlock()

	if !enabled {
		http.NotFound(response, request)
		return
	}

	var body map[string]string
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		http.Error(response, "bad request", http.StatusBadRequest)
		return
	}
	if body["UserName"] != username || body["Password"] != password {
		http.Error(response, "unauthorized", http.StatusUnauthorized)
		return
	}

	state.mu.Lock()
	state.sessionCreated++
	state.mu.Unlock()

	response.Header().Set("X-Auth-Token", "session-token")
	response.WriteHeader(http.StatusCreated)
}

func (state *simulator) handleSystems(response http.ResponseWriter, request *http.Request) {
	if !state.authorize(response, request) {
		return
	}
	writeJSON(response, map[string]any{
		"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/1"}},
	})
}

func (state *simulator) handleSystem(response http.ResponseWriter, request *http.Request) {
	if !state.authorize(response, request) {
		return
	}
	switch request.Method {
	case http.MethodGet:
		snapshot := state.snapshot()
		writeJSON(response, map[string]any{
			"PowerState": "On",
			"Boot": map[string]any{
				"BootSourceOverrideTarget@Redfish.AllowableValues": []string{"Pxe", "UefiHttp", "Cd"},
				"BootSourceOverrideEnabled":                        snapshot.BootEnabled,
				"BootSourceOverrideTarget":                         snapshot.BootTarget,
				"BootOrder":                                        snapshot.NormalBootOrder,
			},
			"Actions": map[string]any{
				"#ComputerSystem.Reset": map[string]string{
					"target": "/redfish/v1/Systems/1/Actions/ComputerSystem.Reset",
				},
			},
		})
	case http.MethodPatch:
		var body bootPatchRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			http.Error(response, "bad request", http.StatusBadRequest)
			return
		}

		state.mu.Lock()
		state.bootPatchCount++
		state.bootEnabled = body.Boot.Enabled
		state.bootTarget = body.Boot.Target
		state.mu.Unlock()

		response.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(response, request)
	}
}

func (state *simulator) handleReset(response http.ResponseWriter, request *http.Request) {
	if !state.authorize(response, request) {
		return
	}
	if request.Method != http.MethodPost {
		http.NotFound(response, request)
		return
	}

	var body resetRequest
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		http.Error(response, "bad request", http.StatusBadRequest)
		return
	}

	state.mu.Lock()
	if body.ResetType == "On" {
		usedTarget := state.currentBootTargetLocked()
		if usedTarget != "" {
			state.bootHistory = append(state.bootHistory, usedTarget)
		}
		if state.bootEnabled == "Once" {
			state.bootConsumeCount++
			state.bootEnabled = "Disabled"
			state.bootTarget = ""
		}
	}
	state.mu.Unlock()
	response.WriteHeader(http.StatusNoContent)
}

func (state *simulator) handleManagers(response http.ResponseWriter, request *http.Request) {
	if !state.authorize(response, request) {
		return
	}
	writeJSON(response, map[string]any{
		"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/1"}},
	})
}

func (state *simulator) handleManager(response http.ResponseWriter, request *http.Request) {
	if !state.authorize(response, request) {
		return
	}
	writeJSON(response, map[string]any{
		"VirtualMedia": map[string]string{"@odata.id": "/redfish/v1/Managers/1/VirtualMedia"},
	})
}

func (state *simulator) handleVirtualMediaCollection(response http.ResponseWriter, request *http.Request) {
	if !state.authorize(response, request) {
		return
	}
	writeJSON(response, map[string]any{
		"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/1/VirtualMedia/CD1"}},
	})
}

func (state *simulator) handleVirtualMedia(response http.ResponseWriter, request *http.Request) {
	if !state.authorize(response, request) {
		return
	}
	snapshot := state.snapshot()
	writeJSON(response, map[string]any{
		"MediaTypes": []string{"CD"},
		"Image":      snapshot.MediaImage,
		"Inserted":   snapshot.MediaInserted,
		"Oem": map[string]any{
			"TART": map[string]string{"OperationID": snapshot.MediaOperationID},
		},
		"Actions": map[string]any{
			"#VirtualMedia.InsertMedia": map[string]string{
				"target": "/redfish/v1/Managers/1/VirtualMedia/CD1/Actions/VirtualMedia.InsertMedia",
			},
			"#VirtualMedia.EjectMedia": map[string]string{
				"target": "/redfish/v1/Managers/1/VirtualMedia/CD1/Actions/VirtualMedia.EjectMedia",
			},
		},
	})
}

func (state *simulator) handleInsertMedia(response http.ResponseWriter, request *http.Request) {
	if !state.authorize(response, request) {
		return
	}
	if request.Method != http.MethodPost {
		http.NotFound(response, request)
		return
	}

	var body insertMediaRequest
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		http.Error(response, "bad request", http.StatusBadRequest)
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.mediaInserted && (state.mediaImage != body.Image || state.mediaOperationID != body.Oem.TART.OperationID) {
		http.Error(response, "conflict", http.StatusConflict)
		return
	}
	if state.mediaInserted && state.mediaImage == body.Image && state.mediaOperationID == body.Oem.TART.OperationID {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	state.insertCount++
	state.mediaInserted = true
	state.mediaImage = body.Image
	state.mediaOperationID = body.Oem.TART.OperationID
	response.WriteHeader(http.StatusNoContent)
}

func (state *simulator) handleEjectMedia(response http.ResponseWriter, request *http.Request) {
	if !state.authorize(response, request) {
		return
	}
	if request.Method != http.MethodPost {
		http.NotFound(response, request)
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.mediaInserted {
		state.ejectCount++
	}
	state.mediaInserted = false
	state.mediaImage = ""
	state.mediaOperationID = ""
	response.WriteHeader(http.StatusNoContent)
}

func (state *simulator) authorize(response http.ResponseWriter, request *http.Request) bool {
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.sessionSupported {
		if request.Header.Get("X-Auth-Token") == "session-token" {
			state.tokenAuthRequests++
			return true
		}
		http.Error(response, "unauthorized", http.StatusUnauthorized)
		return false
	}

	user, pass, ok := request.BasicAuth()
	if ok && user == username && pass == password {
		state.basicAuthRequests++
		return true
	}
	http.Error(response, "unauthorized", http.StatusUnauthorized)
	return false
}

func (state *simulator) snapshot() debugState {
	state.mu.Lock()
	defer state.mu.Unlock()
	return debugState{
		SessionSupported:  state.sessionSupported,
		SessionAttempts:   state.sessionAttempts,
		SessionCreated:    state.sessionCreated,
		BasicAuthRequests: state.basicAuthRequests,
		TokenAuthRequests: state.tokenAuthRequests,
		BootPatchCount:    state.bootPatchCount,
		BootConsumeCount:  state.bootConsumeCount,
		InsertCount:       state.insertCount,
		EjectCount:        state.ejectCount,
		NormalBootOrder:   append([]string(nil), state.normalBootOrder...),
		BootHistory:       append([]string(nil), state.bootHistory...),
		BootEnabled:       state.bootEnabled,
		BootTarget:        state.bootTarget,
		MediaInserted:     state.mediaInserted,
		MediaImage:        state.mediaImage,
		MediaOperationID:  state.mediaOperationID,
	}
}

func (state *simulator) currentBootTargetLocked() string {
	if state.bootEnabled == "Once" || state.bootEnabled == "Continuous" {
		return state.bootTarget
	}
	if len(state.normalBootOrder) == 0 {
		return ""
	}
	return state.normalBootOrder[0]
}

func writeJSON(response http.ResponseWriter, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(response).Encode(body); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func writeReadyFile(path string, payload readyPayload) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.Printf("close ready file: %v", closeErr)
		}
	}()
	return json.NewEncoder(file).Encode(payload)
}

func generateCertificate() ([]byte, []byte, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "127.0.0.1",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate: %w", err)
	}

	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	return certificatePEM, keyPEM, nil
}
