package host

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"uuid"
)

var ErrInvalidID = errors.New("invalid host ID")

// HostIDはTartHostに割り当てられた永続的な識別子である。
type HostID uuid.UUID

// ParseHostIDはHost IDを検証して構築する。
func ParseHostID(value string) (HostID, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil || parsed == uuid.Nil() {
		return HostID{}, fmt.Errorf("%w: %q", ErrInvalidID, value)
	}
	return HostID(parsed), nil
}

// NewHostIDは新しいHost IDを生成する。
func NewHostID() HostID {
	parsed, err := ParseHostID(uuid.NewV4().String())
	if err != nil {
		panic("generated host ID failed validation")
	}
	return parsed
}

// IsZeroはHost IDが未設定かを返す。
func (id HostID) IsZero() bool {
	return uuid.UUID(id) == uuid.Nil()
}

// StringはHost IDの正規化済み表現を返す。
func (id HostID) String() string {
	return uuid.UUID(id).String()
}

func (id HostID) MarshalJSON() ([]byte, error) {
	return strconv.AppendQuote(nil, id.String()), nil
}

func (id *HostID) UnmarshalJSON(value []byte) error {
	return unmarshalTextJSON(value, id.UnmarshalText)
}

func (id HostID) MarshalText() ([]byte, error) {
	return []byte(id.String()), nil
}

func (id *HostID) UnmarshalText(value []byte) error {
	if len(value) == 0 {
		*id = HostID{}
		return nil
	}
	parsed, err := ParseHostID(string(value))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

func unmarshalTextJSON(value []byte, unmarshal func([]byte) error) error {
	text, err := strconv.Unquote(string(value))
	if err != nil {
		return err
	}
	return unmarshal([]byte(text))
}
