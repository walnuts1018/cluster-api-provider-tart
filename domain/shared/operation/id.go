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

package operation

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var ErrInvalidID = errors.New("invalid operation ID")

var deterministicNamespace = uuid.MustParse("6a05d37e-dbc6-43d7-89e3-b0b3b3518348")

type ID struct {
	value uuid.UUID
}

func DeterministicID(key string) (ID, error) {
	if key == "" {
		return ID{}, fmt.Errorf("%w: deterministic key must not be empty", ErrInvalidID)
	}
	return ID{value: uuid.NewSHA1(deterministicNamespace, []byte(key))}, nil
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
