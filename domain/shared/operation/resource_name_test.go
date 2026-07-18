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
	"testing"
)

func TestResourceName(t *testing.T) {
	t.Parallel()

	first, err := ResourceName("host-a-uid")
	if err != nil {
		t.Fatalf("ResourceName() error = %v", err)
	}
	second, err := ResourceName("host-a-uid")
	if err != nil {
		t.Fatalf("ResourceName() error = %v", err)
	}
	other, err := ResourceName("host-b-uid")
	if err != nil {
		t.Fatalf("ResourceName() error = %v", err)
	}

	if first != second {
		t.Fatalf("同じHost UIDの名前が一致しません: %q != %q", first, second)
	}
	if first == other {
		t.Fatalf("異なるHost UIDの名前が一致しました: %q", first)
	}
	if len(first) != 63 {
		t.Fatalf("名前の長さ = %d, want 63", len(first))
	}
}

func TestResourceNameRejectsEmptyHostUID(t *testing.T) {
	t.Parallel()

	if _, err := ResourceName(""); !errors.Is(err, ErrInvalidHostUID) {
		t.Fatalf("ResourceName() error = %v, want %v", err, ErrInvalidHostUID)
	}
}
