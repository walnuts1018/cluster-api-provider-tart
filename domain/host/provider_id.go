package host

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var ErrInvalidProviderID = errors.New("invalid Host ProviderID")

// ProviderIDはHost IDから決定論的に生成されるCAPI ProviderIDである。
type ProviderID string

// NewProviderIDはHost IDからProviderIDを生成する。
func NewProviderID(hostID HostID) (ProviderID, error) {
	if hostID.IsZero() {
		return "", fmt.Errorf("%w: host ID is empty", ErrInvalidProviderID)
	}
	return ProviderID("tart://host/" + hostID.String()), nil
}

// ParseProviderIDはTartのProviderID形式を検証して正規化する。
func ParseProviderID(value string) (ProviderID, error) {
	const prefix = "tart://host/"
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, prefix) {
		return "", fmt.Errorf("%w: %q", ErrInvalidProviderID, value)
	}
	hostID, err := ParseHostID(strings.TrimPrefix(value, prefix))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidProviderID, err)
	}
	return NewProviderID(hostID)
}

func (id ProviderID) IsZero() bool {
	return id == ""
}

func (id ProviderID) String() string {
	return string(id)
}

func (id ProviderID) MarshalJSON() ([]byte, error) {
	return strconv.AppendQuote(nil, id.String()), nil
}

func (id *ProviderID) UnmarshalJSON(value []byte) error {
	return unmarshalTextJSON(value, id.UnmarshalText)
}

func (id ProviderID) MarshalText() ([]byte, error) {
	return []byte(id.String()), nil
}

func (id *ProviderID) UnmarshalText(value []byte) error {
	if len(value) == 0 {
		*id = ""
		return nil
	}
	parsed, err := ParseProviderID(string(value))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}
