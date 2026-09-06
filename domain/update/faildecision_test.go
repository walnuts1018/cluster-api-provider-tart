package update

import (
	"testing"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
)

// TestDecideRejectsUnrecognizedChangeClassは、既知の4つのChangeClass以外が渡された場合、
// ChangeUpdatableと同じ経路へ暗黙に進めず、errorを返してfail-closedで停止することを検証する。
// classify.goが将来新しいChangeClassを追加した際、Decideの分岐を更新し忘れても安全側に倒れる
// ことを保証する回帰テストである。
func TestDecideRejectsUnrecognizedChangeClass(t *testing.T) {
	t.Parallel()

	decision, err := Decide(ChangeClass("SomeFutureClass"), "unused", bootstrapv1alpha1.ConfigurationApplyStrategyStagedReboot)
	if err == nil {
		t.Fatal("Decide() error = nil, want an error for an unrecognized change class")
	}
	if decision.Class != "" {
		t.Fatalf("Decide() decision = %#v, want a zero-value Decision on error", decision)
	}
}

func TestDecideKnownClasses(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		class ChangeClass
		want  ChangeClass
	}{
		"none":                 {class: ChangeNone, want: ChangeNone},
		"invariant conflict":   {class: ChangeInvariantConflict, want: ChangeInvariantConflict},
		"reprovision required": {class: ChangeReprovisionRequired, want: ChangeReprovisionRequired},
		"updatable":            {class: ChangeUpdatable, want: ChangeUpdatable},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			decision, err := Decide(tt.class, "reason", bootstrapv1alpha1.ConfigurationApplyStrategyStagedReboot)
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			if decision.Class != tt.want {
				t.Fatalf("Decide() class = %v, want %v", decision.Class, tt.want)
			}
		})
	}
}
