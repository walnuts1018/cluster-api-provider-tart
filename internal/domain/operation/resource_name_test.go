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
