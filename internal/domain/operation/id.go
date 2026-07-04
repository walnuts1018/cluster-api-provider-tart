package operation

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var ErrInvalidID = errors.New("invalid operation ID")

type ID struct {
	value uuid.UUID
}

func ParseID(value string) (ID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed == uuid.Nil {
		return ID{}, fmt.Errorf("%w: %q", ErrInvalidID, value)
	}
	return ID{value: parsed}, nil
}

func (id ID) String() string {
	return id.value.String()
}

func (id ID) Valid() bool {
	return id.value != uuid.Nil
}
