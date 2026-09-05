package capability

import (
	"errors"
	"fmt"
)

var ErrUnknown = errors.New("unknown capability")

type Capability string

const (
	PowerOn           Capability = "PowerOn"
	PowerOff          Capability = "PowerOff"
	ObservePowerState Capability = "ObservePowerState"
	SetNextBoot       Capability = "SetNextBoot"
	VirtualMedia      Capability = "VirtualMedia"
)

var all = []Capability{
	PowerOn,
	PowerOff,
	ObservePowerState,
	SetNextBoot,
	VirtualMedia,
}

func All() []Capability {
	return append([]Capability(nil), all...)
}

func Parse(value string) (Capability, error) {
	capability := Capability(value)
	if !capability.Valid() {
		return "", fmt.Errorf("%w: %q", ErrUnknown, value)
	}
	return capability, nil
}

func (c Capability) Valid() bool {
	switch c {
	case PowerOn, PowerOff, ObservePowerState, SetNextBoot, VirtualMedia:
		return true
	case "":
		return false
	}
	return false
}

type Set struct {
	values map[Capability]struct{}
}

func NewSet(capabilities ...Capability) (Set, error) {
	values := make(map[Capability]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if !capability.Valid() {
			return Set{}, fmt.Errorf("%w: %q", ErrUnknown, capability)
		}
		values[capability] = struct{}{}
	}
	return Set{values: values}, nil
}

func (s Set) Has(capability Capability) bool {
	_, ok := s.values[capability]
	return ok
}

func (s Set) ContainsAll(required Set) bool {
	for capability := range required.values {
		if !s.Has(capability) {
			return false
		}
	}
	return true
}

func (s Set) Values() []Capability {
	values := make([]Capability, 0, len(s.values))
	for _, capability := range all {
		if s.Has(capability) {
			values = append(values, capability)
		}
	}
	return values
}
