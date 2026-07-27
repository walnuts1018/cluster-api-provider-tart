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

package redfish

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	capabilitydomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/capability"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/driver"
)

type Adapter struct {
	baseTransport http.RoundTripper
	mu            sync.Mutex
	sessions      map[string]*cachedSession
}

type cachedSession struct {
	client  *http.Client
	session *session
}

func New() *Adapter {
	return &Adapter{
		baseTransport: http.DefaultTransport,
		sessions:      make(map[string]*cachedSession),
	}
}

func NewWithTransport(transport http.RoundTripper) *Adapter {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &Adapter{
		baseTransport: transport,
		sessions:      make(map[string]*cachedSession),
	}
}

func (adapter *Adapter) DiscoverCapabilities(
	ctx context.Context,
	_ driverdomain.Name,
	target driverdomain.HostTarget,
	_ applicationdriver.Invocation,
) (capabilitydomain.Set, error) {
	return executeWithSession(adapter, ctx, target, func(client *http.Client, session *session) (capabilitydomain.Set, error) {
		system, media, err := session.discover(ctx, client)
		if err != nil {
			return capabilitydomain.Set{}, err
		}

		capabilities := []capabilitydomain.Capability{capabilitydomain.ObservePowerState}
		if system.ResetAction() != "" {
			capabilities = append(capabilities, capabilitydomain.PowerOn, capabilitydomain.PowerOff)
		}
		if system.canSetBoot() {
			capabilities = append(capabilities, capabilitydomain.SetNextBoot)
		}
		if media.InsertAction() != "" && media.EjectAction() != "" {
			capabilities = append(capabilities, capabilitydomain.VirtualMedia)
		}
		return capabilitydomain.NewSet(capabilities...)
	})
}

func (adapter *Adapter) PowerOn(
	ctx context.Context,
	target driverdomain.HostTarget,
	_ operationdomain.ID,
) error {
	return adapter.reset(ctx, target, "On")
}

func (adapter *Adapter) PowerOff(
	ctx context.Context,
	target driverdomain.HostTarget,
	_ operationdomain.ID,
) error {
	return adapter.reset(ctx, target, "GracefulShutdown")
}

func (adapter *Adapter) ObservePowerState(
	ctx context.Context,
	target driverdomain.HostTarget,
) (driverdomain.PowerState, error) {
	return executeWithSession(adapter, ctx, target, func(client *http.Client, session *session) (driverdomain.PowerState, error) {
		system, _, err := session.discover(ctx, client)
		if err != nil {
			return driverdomain.PowerStateUnknown, err
		}
		switch strings.ToLower(system.PowerState) {
		case "on":
			return driverdomain.PowerStateOn, nil
		case "off":
			return driverdomain.PowerStateOff, nil
		default:
			return driverdomain.PowerStateUnknown, nil
		}
	})
}

func (adapter *Adapter) ObserveBootState(
	ctx context.Context,
	target driverdomain.HostTarget,
) (driverdomain.BootState, error) {
	return executeWithSession(adapter, ctx, target, func(client *http.Client, session *session) (driverdomain.BootState, error) {
		system, media, err := session.discover(ctx, client)
		if err != nil {
			return driverdomain.BootState{}, err
		}

		state := driverdomain.BootState{
			OverrideEnabled: strings.EqualFold(system.Boot.OverrideEnabled, "Once") ||
				strings.EqualFold(system.Boot.OverrideEnabled, "Continuous"),
		}
		targetValue, err := bootTargetFromOverride(system.Boot.OverrideTarget)
		if err != nil {
			return driverdomain.BootState{}, err
		}
		state.OverrideTarget = targetValue

		if media.Path != "" {
			inserted, err := session.getVirtualMedia(ctx, client, media.Path)
			if err != nil {
				return driverdomain.BootState{}, err
			}
			state.MediaInserted = inserted.Inserted
			state.MediaImage = inserted.Image
			state.MediaOperation = inserted.Oem.TART.OperationID
		}
		return state, nil
	})
}

func (adapter *Adapter) SetNextBoot(
	ctx context.Context,
	target driverdomain.HostTarget,
	bootTarget driverdomain.BootTarget,
	_ operationdomain.ID,
) error {
	return executeWithSessionNoReturn(adapter, ctx, target, func(client *http.Client, session *session) error {
		system, _, err := session.discover(ctx, client)
		if err != nil {
			return err
		}
		value, err := bootOverrideValue(system, bootTarget)
		if err != nil {
			return err
		}
		payload := map[string]any{
			"Boot": map[string]any{
				"BootSourceOverrideEnabled": "Once",
				"BootSourceOverrideTarget":  value,
			},
		}
		return session.patch(ctx, client, system.Path, payload, http.StatusOK, http.StatusAccepted, http.StatusNoContent)
	})
}

func (adapter *Adapter) Mount(
	ctx context.Context,
	target driverdomain.HostTarget,
	artifact driverdomain.Artifact,
	operationID operationdomain.ID,
) error {
	return executeWithSessionNoReturn(adapter, ctx, target, func(client *http.Client, session *session) error {
		_, media, err := session.discover(ctx, client)
		if err != nil {
			return err
		}
		if media.Path == "" || media.InsertAction() == "" || media.EjectAction() == "" {
			return driverdomain.NewError(driverdomain.ErrorUnsupported, errors.New("virtual media is not supported"))
		}
		inserted, err := session.getVirtualMedia(ctx, client, media.Path)
		if err != nil {
			return err
		}
		if inserted.Inserted && inserted.Image == artifact.Reference() {
			if inserted.Oem.TART.OperationID == "" || inserted.Oem.TART.OperationID == operationID.String() {
				return nil
			}
		}
		if inserted.Inserted {
			return driverdomain.NewError(
				driverdomain.ErrorConflict,
				fmt.Errorf("virtual media %q is already mounted for operation %s", inserted.Image, inserted.Oem.TART.OperationID),
			)
		}
		return session.post(ctx, client, media.InsertAction(), map[string]any{
			"Image":          artifact.Reference(),
			"Inserted":       true,
			"WriteProtected": true,
			"Oem":            map[string]any{"TART": map[string]any{"OperationID": operationID.String()}},
		}, http.StatusOK, http.StatusAccepted, http.StatusNoContent)
	})
}

func (adapter *Adapter) Unmount(
	ctx context.Context,
	target driverdomain.HostTarget,
	_ operationdomain.ID,
) error {
	return executeWithSessionNoReturn(adapter, ctx, target, func(client *http.Client, session *session) error {
		_, media, err := session.discover(ctx, client)
		if err != nil {
			return err
		}
		if media.Path == "" || media.EjectAction() == "" {
			return driverdomain.NewError(driverdomain.ErrorUnsupported, errors.New("virtual media is not supported"))
		}
		inserted, err := session.getVirtualMedia(ctx, client, media.Path)
		if err != nil {
			return err
		}
		if !inserted.Inserted {
			return nil
		}
		return session.post(ctx, client, media.EjectAction(), map[string]any{}, http.StatusOK, http.StatusAccepted, http.StatusNoContent)
	})
}

func (adapter *Adapter) reset(ctx context.Context, target driverdomain.HostTarget, resetType string) error {
	return executeWithSessionNoReturn(adapter, ctx, target, func(client *http.Client, session *session) error {
		system, _, err := session.discover(ctx, client)
		if err != nil {
			return err
		}
		if system.ResetAction() == "" {
			return driverdomain.NewError(driverdomain.ErrorUnsupported, errors.New("reset action is not supported"))
		}
		return session.post(ctx, client, system.ResetAction(), map[string]string{"ResetType": resetType}, http.StatusOK, http.StatusAccepted, http.StatusNoContent)
	})
}

type session struct {
	access    driverdomain.RedfishAccess
	authToken string
	useBasic  bool
}

type serviceRoot struct {
	Systems        link `json:"Systems"`
	Managers       link `json:"Managers"`
	SessionService link `json:"SessionService"`
}

type link struct {
	ODataID string `json:"@odata.id"`
}

type collection struct {
	Members []link `json:"Members"`
}

type systemResource struct {
	Path       string `json:"-"`
	PowerState string `json:"PowerState"`
	Boot       struct {
		Allowed         []string `json:"BootSourceOverrideTarget@Redfish.AllowableValues"`
		OverrideEnabled string   `json:"BootSourceOverrideEnabled"`
		OverrideTarget  string   `json:"BootSourceOverrideTarget"`
	} `json:"Boot"`
	Actions struct {
		Reset struct {
			Target string `json:"target"`
		} `json:"#ComputerSystem.Reset"`
	} `json:"Actions"`
}

func (system systemResource) ResetAction() string {
	return system.Actions.Reset.Target
}

func (system systemResource) canSetBoot() bool {
	_, err := bootOverrideValue(system, driverdomain.BootTargetPXE)
	if err == nil {
		return true
	}
	_, err = bootOverrideValue(system, driverdomain.BootTargetHTTP)
	if err == nil {
		return true
	}
	_, err = bootOverrideValue(system, driverdomain.BootTargetVirtualMedia)
	return err == nil
}

type virtualMediaCollection struct {
	Members []link `json:"Members"`
}

type virtualMediaResource struct {
	Path       string   `json:"-"`
	MediaTypes []string `json:"MediaTypes"`
	Image      string   `json:"Image"`
	Inserted   bool     `json:"Inserted"`
	Oem        struct {
		TART struct {
			OperationID string `json:"OperationID"`
		} `json:"TART"`
	} `json:"Oem"`
	Actions struct {
		InsertMedia struct {
			Target string `json:"target"`
		} `json:"#VirtualMedia.InsertMedia"`
		EjectMedia struct {
			Target string `json:"target"`
		} `json:"#VirtualMedia.EjectMedia"`
	} `json:"Actions"`
}

func (virtualMedia virtualMediaResource) InsertAction() string {
	return virtualMedia.Actions.InsertMedia.Target
}

func (virtualMedia virtualMediaResource) EjectAction() string {
	return virtualMedia.Actions.EjectMedia.Target
}

func sessionCacheKey(access driverdomain.RedfishAccess) string {
	return access.Endpoint() + "|" + access.Username()
}

func (adapter *Adapter) getSession(ctx context.Context, target driverdomain.HostTarget) (*http.Client, *session, error) {
	access, ok := target.RedfishAccess()
	if !ok {
		return nil, nil, driverdomain.NewError(driverdomain.ErrorUnsupported, errors.New("redfish access is not configured"))
	}
	key := sessionCacheKey(access)

	adapter.mu.Lock()
	if adapter.sessions == nil {
		adapter.sessions = make(map[string]*cachedSession)
	}
	cached, ok := adapter.sessions[key]
	adapter.mu.Unlock()

	if ok {
		return cached.client, cached.session, nil
	}

	client, err := adapter.newHTTPClient(access)
	if err != nil {
		return nil, nil, err
	}
	sess := &session{access: access}
	if err := sess.authenticate(ctx, client); err != nil {
		return nil, nil, err
	}

	adapter.mu.Lock()
	adapter.sessions[key] = &cachedSession{
		client:  client,
		session: sess,
	}
	adapter.mu.Unlock()

	return client, sess, nil
}

func (adapter *Adapter) clearSession(target driverdomain.HostTarget) {
	access, ok := target.RedfishAccess()
	if !ok {
		return
	}
	key := sessionCacheKey(access)
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.sessions != nil {
		delete(adapter.sessions, key)
	}
}

func executeWithSession[T any](adapter *Adapter, ctx context.Context, target driverdomain.HostTarget, fn func(*http.Client, *session) (T, error)) (T, error) {
	client, sess, err := adapter.getSession(ctx, target)
	if err != nil {
		var zero T
		return zero, err
	}
	res, err := fn(client, sess)
	if err != nil && driverdomain.IsErrorKind(err, driverdomain.ErrorAuthenticationFailed) {
		adapter.clearSession(target)
		client, sess, err = adapter.getSession(ctx, target)
		if err != nil {
			var zero T
			return zero, err
		}
		return fn(client, sess)
	}
	return res, err
}

func executeWithSessionNoReturn(adapter *Adapter, ctx context.Context, target driverdomain.HostTarget, fn func(*http.Client, *session) error) error {
	_, err := executeWithSession(adapter, ctx, target, func(client *http.Client, sess *session) (struct{}, error) {
		return struct{}{}, fn(client, sess)
	})
	return err
}

func (adapter *Adapter) newHTTPClient(access driverdomain.RedfishAccess) (*http.Client, error) {
	base, ok := adapter.baseTransport.(*http.Transport)
	if !ok {
		return &http.Client{Transport: adapter.baseTransport}, nil
	}
	transport := base.Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	}
	if bundle := access.CABundlePEM(); len(bundle) > 0 {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(bundle) {
			return nil, driverdomain.NewError(driverdomain.ErrorTLSVerificationFailed, errors.New("invalid CA bundle PEM"))
		}
		transport.TLSClientConfig.RootCAs = pool
	}
	pins := access.SPKIPins()
	if len(pins) > 0 {
		transport.TLSClientConfig.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return driverdomain.NewError(driverdomain.ErrorTLSVerificationFailed, errors.New("missing peer certificate"))
			}
			sum := sha256.Sum256(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
			got := base64.StdEncoding.EncodeToString(sum[:])
			for _, pin := range pins {
				normalized := strings.TrimPrefix(pin, "sha256:")
				if normalized == got {
					return nil
				}
			}
			return driverdomain.NewError(
				driverdomain.ErrorTLSVerificationFailed,
				fmt.Errorf("SPKI pin mismatch: got sha256:%s", got),
			)
		}
	}
	return &http.Client{Transport: transport}, nil
}

func (session *session) authenticate(ctx context.Context, client *http.Client) error {
	root := serviceRoot{}
	if err := session.get(ctx, client, "/redfish/v1/", &root, http.StatusOK); err != nil {
		return err
	}
	if root.SessionService.ODataID == "" {
		session.useBasic = true
		return nil
	}
	sessionsPath, err := url.JoinPath(root.SessionService.ODataID, "Sessions")
	if err != nil {
		return driverdomain.NewError(driverdomain.ErrorTemporary, fmt.Errorf("build sessions path: %w", err))
	}
	status, err := session.postForStatus(ctx, client, sessionsPath, map[string]string{
		"UserName": session.access.Username(),
		"Password": session.access.Password(),
	})
	if err != nil {
		return err
	}
	switch status.code {
	case http.StatusCreated, http.StatusOK:
		session.authToken = status.header.Get("X-Auth-Token")
		if session.authToken == "" {
			return driverdomain.NewError(driverdomain.ErrorAuthenticationFailed, errors.New("session authentication did not return X-Auth-Token"))
		}
		return nil
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		session.useBasic = true
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return driverdomain.NewError(driverdomain.ErrorAuthenticationFailed, errors.New("redfish authentication failed"))
	default:
		return classifyHTTPStatus(status.code)
	}
}

type httpStatus struct {
	code   int
	header http.Header
}

func (session *session) postForStatus(
	ctx context.Context,
	client *http.Client,
	path string,
	body any,
) (httpStatus, error) {
	request, err := session.request(ctx, http.MethodPost, path, body)
	if err != nil {
		return httpStatus{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return httpStatus{}, classifyHTTPError(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	if err := response.Body.Close(); err != nil {
		return httpStatus{}, driverdomain.NewError(driverdomain.ErrorTemporary, fmt.Errorf("close Redfish response: %w", err))
	}
	return httpStatus{code: response.StatusCode, header: response.Header.Clone()}, nil
}

func (session *session) discover(
	ctx context.Context,
	client *http.Client,
) (systemResource, virtualMediaResource, error) {
	root := serviceRoot{}
	if err := session.get(ctx, client, "/redfish/v1/", &root, http.StatusOK); err != nil {
		return systemResource{}, virtualMediaResource{}, err
	}
	if root.Systems.ODataID == "" {
		return systemResource{}, virtualMediaResource{}, driverdomain.NewError(driverdomain.ErrorUnsupported, errors.New("systems collection is missing"))
	}
	systems := collection{}
	if err := session.get(ctx, client, root.Systems.ODataID, &systems, http.StatusOK); err != nil {
		return systemResource{}, virtualMediaResource{}, err
	}
	if len(systems.Members) == 0 || systems.Members[0].ODataID == "" {
		return systemResource{}, virtualMediaResource{}, driverdomain.NewError(driverdomain.ErrorUnsupported, errors.New("no ComputerSystem members were published"))
	}
	system := systemResource{}
	if err := session.get(ctx, client, systems.Members[0].ODataID, &system, http.StatusOK); err != nil {
		return systemResource{}, virtualMediaResource{}, err
	}
	system.Path = systems.Members[0].ODataID

	virtualMedia, err := session.discoverVirtualMedia(ctx, client, root.Managers.ODataID)
	if err != nil {
		if driverdomain.IsErrorKind(err, driverdomain.ErrorUnsupported) {
			return system, virtualMediaResource{}, nil
		}
		return systemResource{}, virtualMediaResource{}, err
	}
	return system, virtualMedia, nil
}

func (session *session) discoverVirtualMedia(
	ctx context.Context,
	client *http.Client,
	managersPath string,
) (virtualMediaResource, error) {
	if managersPath == "" {
		return virtualMediaResource{}, driverdomain.NewError(driverdomain.ErrorUnsupported, errors.New("managers collection is missing"))
	}
	managers := collection{}
	if err := session.get(ctx, client, managersPath, &managers, http.StatusOK); err != nil {
		return virtualMediaResource{}, err
	}
	if len(managers.Members) == 0 || managers.Members[0].ODataID == "" {
		return virtualMediaResource{}, driverdomain.NewError(driverdomain.ErrorUnsupported, errors.New("no Manager members were published"))
	}
	manager := struct {
		VirtualMedia link `json:"VirtualMedia"`
	}{}
	if err := session.get(ctx, client, managers.Members[0].ODataID, &manager, http.StatusOK); err != nil {
		return virtualMediaResource{}, err
	}
	if manager.VirtualMedia.ODataID == "" {
		return virtualMediaResource{}, driverdomain.NewError(driverdomain.ErrorUnsupported, errors.New("VirtualMedia collection is missing"))
	}
	collection := virtualMediaCollection{}
	if err := session.get(ctx, client, manager.VirtualMedia.ODataID, &collection, http.StatusOK); err != nil {
		return virtualMediaResource{}, err
	}
	for _, member := range collection.Members {
		candidate := virtualMediaResource{}
		if err := session.get(ctx, client, member.ODataID, &candidate, http.StatusOK); err != nil {
			return virtualMediaResource{}, err
		}
		if supportsCD(candidate.MediaTypes) {
			candidate.Path = member.ODataID
			return candidate, nil
		}
	}
	return virtualMediaResource{}, driverdomain.NewError(driverdomain.ErrorUnsupported, errors.New("no CD/DVD virtual media device is available"))
}

func (session *session) getVirtualMedia(
	ctx context.Context,
	client *http.Client,
	path string,
) (virtualMediaResource, error) {
	virtualMedia := virtualMediaResource{}
	if err := session.get(ctx, client, path, &virtualMedia, http.StatusOK); err != nil {
		return virtualMediaResource{}, err
	}
	virtualMedia.Path = path
	return virtualMedia, nil
}

func supportsCD(mediaTypes []string) bool {
	for _, mediaType := range mediaTypes {
		switch strings.ToLower(mediaType) {
		case "cd", "dvd":
			return true
		}
	}
	return false
}

func bootOverrideValue(system systemResource, target driverdomain.BootTarget) (string, error) {
	want := ""
	switch target {
	case driverdomain.BootTargetPXE:
		want = "Pxe"
	case driverdomain.BootTargetHTTP:
		want = "UefiHttp"
	case driverdomain.BootTargetVirtualMedia:
		want = "Cd"
	default:
		return "", driverdomain.NewError(driverdomain.ErrorUnsupported, fmt.Errorf("unknown boot target %q", target))
	}
	if slices.Contains(system.Boot.Allowed, want) {
		return want, nil
	}
	return "", driverdomain.NewError(driverdomain.ErrorUnsupported, fmt.Errorf("boot target %q is not supported", target))
}

func bootTargetFromOverride(value string) (driverdomain.BootTarget, error) {
	switch value {
	case "":
		return "", nil
	case "Pxe":
		return driverdomain.BootTargetPXE, nil
	case "UefiHttp":
		return driverdomain.BootTargetHTTP, nil
	case "Cd":
		return driverdomain.BootTargetVirtualMedia, nil
	default:
		return "", driverdomain.NewError(driverdomain.ErrorUnsupported, fmt.Errorf("boot override target %q is not supported", value))
	}
}

func (session *session) get(
	ctx context.Context,
	client *http.Client,
	path string,
	into any,
	expected ...int,
) error {
	return session.do(ctx, client, http.MethodGet, path, nil, into, expected...)
}

func (session *session) post(
	ctx context.Context,
	client *http.Client,
	path string,
	body any,
	expected ...int,
) error {
	return session.do(ctx, client, http.MethodPost, path, body, nil, expected...)
}

func (session *session) patch(
	ctx context.Context,
	client *http.Client,
	path string,
	body any,
	expected ...int,
) error {
	return session.do(ctx, client, http.MethodPatch, path, body, nil, expected...)
}

func (session *session) do(
	ctx context.Context,
	client *http.Client,
	method string,
	path string,
	body any,
	into any,
	expected ...int,
) error {
	request, err := session.request(ctx, method, path, body)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return classifyHTTPError(err)
	}
	if !contains(expected, response.StatusCode) {
		_, _ = io.Copy(io.Discard, response.Body)
		if err := response.Body.Close(); err != nil {
			return driverdomain.NewError(driverdomain.ErrorTemporary, fmt.Errorf("close Redfish response: %w", err))
		}
		return classifyHTTPStatus(response.StatusCode)
	}
	if into == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		if err := response.Body.Close(); err != nil {
			return driverdomain.NewError(driverdomain.ErrorTemporary, fmt.Errorf("close Redfish response: %w", err))
		}
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(into); err != nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close Redfish response: %w", closeErr))
		}
		return driverdomain.NewError(driverdomain.ErrorTemporary, fmt.Errorf("decode Redfish response: %w", err))
	}
	if err := response.Body.Close(); err != nil {
		return driverdomain.NewError(driverdomain.ErrorTemporary, fmt.Errorf("close Redfish response: %w", err))
	}
	return nil
}

func (session *session) request(ctx context.Context, method string, path string, body any) (*http.Request, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal Redfish request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}
	fullURL, err := url.JoinPath(session.access.Endpoint(), path)
	if err != nil {
		return nil, fmt.Errorf("build Redfish request URL: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return nil, fmt.Errorf("build Redfish request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if session.authToken != "" {
		request.Header.Set("X-Auth-Token", session.authToken)
	}
	if session.useBasic {
		request.SetBasicAuth(session.access.Username(), session.access.Password())
	}
	return request, nil
}

func contains(values []int, want int) bool {
	return slices.Contains(values, want)
}

func classifyHTTPError(err error) error {
	if _, ok := errors.AsType[x509.UnknownAuthorityError](err); ok {
		return driverdomain.NewError(driverdomain.ErrorTLSVerificationFailed, err)
	}
	if _, ok := errors.AsType[x509.HostnameError](err); ok {
		return driverdomain.NewError(driverdomain.ErrorTLSVerificationFailed, err)
	}
	if _, ok := errors.AsType[x509.CertificateInvalidError](err); ok {
		return driverdomain.NewError(driverdomain.ErrorTLSVerificationFailed, err)
	}
	if _, ok := errors.AsType[*driverdomain.Error](err); ok {
		return err
	}
	return driverdomain.NewError(driverdomain.ErrorTemporary, err)
}

func classifyHTTPStatus(statusCode int) error {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return driverdomain.NewError(driverdomain.ErrorAuthenticationFailed, fmt.Errorf("unexpected Redfish status %d", statusCode))
	case http.StatusConflict:
		return driverdomain.NewError(driverdomain.ErrorConflict, fmt.Errorf("unexpected Redfish status %d", statusCode))
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return driverdomain.NewError(driverdomain.ErrorUnsupported, fmt.Errorf("unexpected Redfish status %d", statusCode))
	default:
		if statusCode >= 500 {
			return driverdomain.NewError(driverdomain.ErrorTemporary, fmt.Errorf("unexpected Redfish status %d", statusCode))
		}
		return driverdomain.NewError(driverdomain.ErrorTemporary, fmt.Errorf("unexpected Redfish status %d", statusCode))
	}
}
