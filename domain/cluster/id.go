package cluster

import (
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/walnuts1018/cluster-api-provider-tart/domain/textmarshal"
)

var ErrInvalidID = errors.New("invalid cluster ID")

// ClusterIDはTartClusterに割り当てられた永続的な識別子である。
type ClusterID uuid.UUID

// ParseClusterIDはCluster IDを検証して構築する。
func ParseClusterID(value string) (ClusterID, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil || parsed == uuid.Nil() {
		return ClusterID{}, fmt.Errorf("%w: %q", ErrInvalidID, value)
	}
	return ClusterID(parsed), nil
}

// NewClusterIDは新しいCluster IDを生成する。
func NewClusterID() ClusterID {
	parsed, err := ParseClusterID(uuid.NewV4().String())
	if err != nil {
		panic("generated cluster ID failed validation")
	}
	return parsed
}

// IsZeroはCluster IDが未設定かを返す。
func (id ClusterID) IsZero() bool {
	return uuid.UUID(id) == uuid.Nil()
}

// StringはCluster IDの正規化済み表現を返す。
func (id ClusterID) String() string {
	return uuid.UUID(id).String()
}

func (id ClusterID) MarshalJSON() ([]byte, error) {
	return textmarshal.JSON(id.String())
}

func (id *ClusterID) UnmarshalJSON(value []byte) error {
	return textmarshal.UnmarshalJSON(value, id.UnmarshalText)
}

func (id ClusterID) MarshalText() ([]byte, error) {
	return []byte(id.String()), nil
}

func (id *ClusterID) UnmarshalText(value []byte) error {
	if len(value) == 0 {
		*id = ClusterID{}
		return nil
	}
	parsed, err := ParseClusterID(string(value))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}
