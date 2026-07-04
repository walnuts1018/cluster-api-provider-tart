package operation

import (
	"crypto/sha256"
	"errors"
	"fmt"
)

var ErrInvalidHostUID = errors.New("invalid host UID")

const activeOperationNamePrefix = "active-"

func ResourceName(hostUID string) (string, error) {
	if hostUID == "" {
		return "", ErrInvalidHostUID
	}
	digest := sha256.Sum256([]byte(hostUID))
	return fmt.Sprintf("%s%x", activeOperationNamePrefix, digest[:28]), nil
}
