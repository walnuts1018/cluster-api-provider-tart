package resourcefinalizer

import (
	"reflect"
	"testing"
)

func TestDecideは期待するFinalizer状態への操作だけを返す(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		desired  DesiredState
		observed ObservedState
		want     Command
	}{
		{
			name:     "必要で存在しない場合は追加する",
			desired:  DesiredPresent{},
			observed: ObservedAbsent{},
			want:     CommandAdd{},
		},
		{
			name:     "必要で存在する場合は何もしない",
			desired:  DesiredPresent{},
			observed: ObservedPresent{},
			want:     CommandNoop{},
		},
		{
			name:     "不要で存在する場合は削除する",
			desired:  DesiredAbsent{},
			observed: ObservedPresent{},
			want:     CommandRemove{},
		},
		{
			name:     "不要で存在しない場合は何もしない",
			desired:  DesiredAbsent{},
			observed: ObservedAbsent{},
			want:     CommandNoop{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Decide(tt.desired, tt.observed)
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Decide() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseNameは空のFinalizer名を拒否する(t *testing.T) {
	t.Parallel()

	if _, err := ParseName(""); err == nil {
		t.Fatal("ParseName(\"\") error = nil, want error")
	}

	got, err := ParseName("infrastructure.cluster.x-k8s.io/example")
	if err != nil {
		t.Fatalf("ParseName() error = %v", err)
	}
	if got.String() != "infrastructure.cluster.x-k8s.io/example" {
		t.Fatalf("ParseName().String() = %q", got.String())
	}
}
