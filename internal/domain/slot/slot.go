package slot

import (
	"errors"
	"fmt"
)

var ErrUnknown = errors.New("unknown OS slot")

type Slot string

const (
	A Slot = "A"
	B Slot = "B"
)

func Parse(value string) (Slot, error) {
	slot := Slot(value)
	switch slot {
	case A, B:
		return slot, nil
	case "":
		return "", fmt.Errorf("%w: %q", ErrUnknown, value)
	}
	return "", fmt.Errorf("%w: %q", ErrUnknown, value)
}

func (s Slot) Inactive() (Slot, error) {
	switch s {
	case A:
		return B, nil
	case B:
		return A, nil
	case "":
		return "", fmt.Errorf("%w: %q", ErrUnknown, s)
	}
	return "", fmt.Errorf("%w: %q", ErrUnknown, s)
}
