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

package agentboot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	agentbootdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/agentboot"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
)

var ErrTargetNotFound = agentbootdomain.ErrTargetNotFound

type Target = agentbootdomain.Target

type Resolver interface {
	Resolve(context.Context, string) (agentbootdomain.Target, error)
}

type Config struct {
	Resolver        Resolver
	Artifact        Artifact
	ArtifactBaseURL string
	AgentAPIURL     string
}

type Handler struct {
	resolver        Resolver
	artifact        Artifact
	artifactBaseURL string
	agentAPIURL     string
	mux             *http.ServeMux
}

func NewHandler(config Config) (*Handler, error) {
	if config.Resolver == nil {
		return nil, errors.New("agent boot target resolver is required")
	}
	if config.Artifact.manifest.Value().Reference == "" {
		return nil, errors.New("verified Agent Artifact is required")
	}
	handler := &Handler{
		resolver:        config.Resolver,
		artifact:        config.Artifact,
		artifactBaseURL: config.ArtifactBaseURL,
		agentAPIURL:     config.AgentAPIURL,
		mux:             http.NewServeMux(),
	}
	if _, err := agentbootdomain.BuildScript(agentbootdomain.ScriptInput{
		ArtifactBaseURL: config.ArtifactBaseURL,
		AgentAPIURL:     config.AgentAPIURL,
		ArtifactDigest:  config.Artifact.digest.String(),
		HostUID:         "validation-host",
		OperationUID:    "validation-operation",
		BootMACAddress:  "00:00:5e:00:53:00",
	}); err != nil {
		return nil, fmt.Errorf("validate Agent boot URLs: %w", err)
	}
	handler.mux.HandleFunc("GET /livez", handler.handleHealth)
	handler.mux.HandleFunc("GET /readyz", handler.handleHealth)
	handler.mux.HandleFunc("GET /ipxe", handler.handleIPXE)
	prefix := fmt.Sprintf("/v1/agent-artifacts/sha256/%s/", config.Artifact.digest.Encoded())
	handler.mux.HandleFunc("GET "+prefix+"kernel", handler.serveKernel)
	handler.mux.HandleFunc("HEAD "+prefix+"kernel", handler.serveKernel)
	handler.mux.HandleFunc("GET "+prefix+"initrd", handler.serveInitrd)
	handler.mux.HandleFunc("HEAD "+prefix+"initrd", handler.serveInitrd)
	if config.Artifact.Manifest().VirtualMedia != nil {
		handler.mux.HandleFunc("GET "+prefix+"virtual-media", handler.serveVirtualMedia)
		handler.mux.HandleFunc("HEAD "+prefix+"virtual-media", handler.serveVirtualMedia)
	}
	return handler, nil
}

func (handler *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	handler.mux.ServeHTTP(response, request)
}

func (handler *Handler) handleHealth(response http.ResponseWriter, _ *http.Request) {
	response.WriteHeader(http.StatusOK)
}

func (handler *Handler) handleIPXE(response http.ResponseWriter, request *http.Request) {
	mac := request.URL.Query().Get("mac")
	if mac == "" {
		http.Error(response, "mac query parameter is required", http.StatusBadRequest)
		return
	}
	target, err := handler.resolver.Resolve(request.Context(), mac)
	if err != nil {
		if errors.Is(err, agentbootdomain.ErrTargetNotFound) ||
			errors.Is(err, agentbootdomain.ErrUnsupportedHost) {
			crlog.FromContext(request.Context()).Info(
				"No eligible Agent boot target found; returning an exit script",
				"mac", mac,
				"reason", err.Error(),
			)
			response.Header().Set("Content-Type", "text/plain; charset=utf-8")
			response.WriteHeader(http.StatusOK)
			_, _ = response.Write([]byte("#!ipxe\nexit\n"))
			return
		}
		crlog.FromContext(request.Context()).Error(err, "Failed to resolve Agent boot target")
		http.Error(response, "failed to resolve Agent boot target", http.StatusInternalServerError)
		return
	}
	if target.PlatformProfile != handler.artifact.Manifest().PlatformProfile {
		crlog.FromContext(request.Context()).Info(
			"Agent Artifact platform profile does not match the boot target; returning an exit script",
			"mac", mac,
			"targetPlatformProfile", target.PlatformProfile,
			"artifactPlatformProfile", handler.artifact.Manifest().PlatformProfile,
		)
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("#!ipxe\nexit\n"))
		return
	}
	script, err := agentbootdomain.BuildScript(agentbootdomain.ScriptInput{
		ArtifactBaseURL: handler.artifactBaseURL,
		AgentAPIURL:     handler.agentAPIURL,
		ArtifactDigest:  handler.artifact.digest.String(),
		HostUID:         target.HostUID,
		OperationUID:    target.OperationUID,
		BootMACAddress:  target.BootMACAddress,
	})
	if err != nil {
		crlog.FromContext(request.Context()).Error(err, "Failed to generate Agent iPXE script")
		http.Error(response, "failed to generate Agent iPXE script", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte(script))
}

func (handler *Handler) serveKernel(response http.ResponseWriter, request *http.Request) {
	handler.serveFile(
		response,
		request,
		"kernel",
		handler.artifact.Manifest().Kernel.Digest,
		"application/vnd.tart.agent-kernel",
	)
}

func (handler *Handler) serveInitrd(response http.ResponseWriter, request *http.Request) {
	handler.serveFile(
		response,
		request,
		"initrd",
		handler.artifact.Manifest().Initrd.Digest,
		"application/vnd.tart.agent-initrd",
	)
}

func (handler *Handler) serveVirtualMedia(response http.ResponseWriter, request *http.Request) {
	descriptor := handler.artifact.Manifest().VirtualMedia
	if descriptor == nil {
		http.NotFound(response, request)
		return
	}
	handler.serveFile(
		response,
		request,
		"virtual-media",
		descriptor.Digest,
		"application/vnd.tart.agent-virtual-media",
	)
}

func (handler *Handler) serveFile(
	response http.ResponseWriter,
	request *http.Request,
	name, payloadDigest, contentType string,
) {
	file, size, err := handler.artifact.payload(name)
	if err != nil {
		crlog.FromContext(request.Context()).Error(err, "Agent Artifact payload is unavailable", "payload", name)
		http.Error(response, "Agent Artifact payload is unavailable", http.StatusServiceUnavailable)
		return
	}
	response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	response.Header().Set("Content-Type", contentType)
	response.Header().Set("ETag", `"`+strings.TrimPrefix(payloadDigest, "sha256:")+`"`)
	http.ServeContent(response, request, name, time.Time{}, io.NewSectionReader(file, 0, size))
}
