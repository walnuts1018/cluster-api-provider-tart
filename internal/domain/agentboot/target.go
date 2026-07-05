package agentboot

import "errors"

var (
	ErrTargetNotFound  = errors.New("agent boot target not found")
	ErrTargetAmbiguous = errors.New("agent boot target is ambiguous")
	ErrUnsupportedHost = errors.New("host does not support the Agent iPXE profile")
)

type Target struct {
	HostUID         string
	OperationUID    string
	BootMACAddress  string
	PlatformProfile string
}
