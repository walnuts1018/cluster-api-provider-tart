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

package driver

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
)

type Name string

const WoL Name = "wol"

const Redfish Name = "redfish"

func ParseName(value string) (Name, error) {
	if value == "" {
		return "", errors.New("driver name must not be empty")
	}
	return Name(value), nil
}

type PowerState string

const (
	PowerStateOn      PowerState = "On"
	PowerStateOff     PowerState = "Off"
	PowerStateUnknown PowerState = "Unknown"
)

type Reachability string

const (
	ReachabilityReachable   Reachability = "Reachable"
	ReachabilityUnreachable Reachability = "Unreachable"
	ReachabilityUnknown     Reachability = "Unknown"
)

type BootTarget string

const (
	BootTargetPXE          BootTarget = "PXE"
	BootTargetHTTP         BootTarget = "HTTP"
	BootTargetVirtualMedia BootTarget = "VirtualMedia"
)

func (target BootTarget) Valid() bool {
	switch target {
	case BootTargetPXE, BootTargetHTTP, BootTargetVirtualMedia:
		return true
	case "":
		return false
	}
	return false
}

type Artifact struct {
	reference string
}

func NewArtifact(reference string) (Artifact, error) {
	if reference == "" {
		return Artifact{}, errors.New("artifact reference must not be empty")
	}
	return Artifact{reference: reference}, nil
}

func (artifact Artifact) Reference() string {
	return artifact.reference
}

type ErrorKind string

const (
	ErrorUnsupported           ErrorKind = "Unsupported"
	ErrorAuthenticationFailed  ErrorKind = "AuthenticationFailed"
	ErrorTLSVerificationFailed ErrorKind = "TLSVerificationFailed"
	ErrorConflict              ErrorKind = "Conflict"
	ErrorTemporary             ErrorKind = "Temporary"
	ErrorDeadlineExceeded      ErrorKind = "DeadlineExceeded"
)

var (
	ErrInvalidEndpoint = errors.New("invalid management endpoint")
	ErrInvalidSPKIPin  = errors.New("invalid SPKI pin")
)

type RedfishAccess struct {
	endpoint    string
	username    string
	password    string
	caBundlePEM []byte
	spkiPins    []string
}

func NewRedfishAccess(
	endpoint string,
	username string,
	password string,
	caBundlePEM []byte,
	spkiPins []string,
) (RedfishAccess, error) {
	if endpoint == "" {
		return RedfishAccess{}, fmt.Errorf("%w: endpoint must not be empty", ErrInvalidEndpoint)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return RedfishAccess{}, fmt.Errorf("%w: %q", ErrInvalidEndpoint, endpoint)
	}
	for _, pin := range spkiPins {
		if pin == "" {
			return RedfishAccess{}, fmt.Errorf("%w: pin must not be empty", ErrInvalidSPKIPin)
		}
		normalized := pin
		if len(normalized) > 7 && normalized[:7] == "sha256:" {
			normalized = normalized[7:]
		}
		decoded, err := base64.StdEncoding.DecodeString(normalized)
		if err != nil || len(decoded) != 32 {
			return RedfishAccess{}, fmt.Errorf("%w: %q", ErrInvalidSPKIPin, pin)
		}
	}
	return RedfishAccess{
		endpoint:    parsed.String(),
		username:    username,
		password:    password,
		caBundlePEM: append([]byte(nil), caBundlePEM...),
		spkiPins:    append([]string(nil), spkiPins...),
	}, nil
}

func (access RedfishAccess) Endpoint() string {
	return access.endpoint
}

func (access RedfishAccess) Username() string {
	return access.username
}

func (access RedfishAccess) Password() string {
	return access.password
}

func (access RedfishAccess) CABundlePEM() []byte {
	return append([]byte(nil), access.caBundlePEM...)
}

func (access RedfishAccess) SPKIPins() []string {
	return append([]string(nil), access.spkiPins...)
}

type Error struct {
	Kind ErrorKind
	Err  error
}

func (err *Error) Error() string {
	if err.Err == nil {
		return string(err.Kind)
	}
	return fmt.Sprintf("%s: %v", err.Kind, err.Err)
}

func (err *Error) Unwrap() error {
	return err.Err
}

func NewError(kind ErrorKind, err error) error {
	return &Error{Kind: kind, Err: err}
}

func IsErrorKind(err error, kind ErrorKind) bool {
	var driverError *Error
	return errors.As(err, &driverError) && driverError.Kind == kind
}
