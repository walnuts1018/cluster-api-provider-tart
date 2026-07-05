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
	"errors"
	"fmt"
)

type Name string

const WoL Name = "wol"

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
	ErrorUnsupported          ErrorKind = "Unsupported"
	ErrorAuthenticationFailed ErrorKind = "AuthenticationFailed"
	ErrorTemporary            ErrorKind = "Temporary"
	ErrorDeadlineExceeded     ErrorKind = "DeadlineExceeded"
)

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
