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
