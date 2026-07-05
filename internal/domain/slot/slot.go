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
