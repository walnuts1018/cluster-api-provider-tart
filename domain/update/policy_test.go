package update

import (
	"errors"
	"testing"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
)

func TestResolveApplyMode(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		policy bootstrapv1alpha1.ConfigurationApplyStrategy
		want   ApplyMode
		err    error
	}{
		"reboot":                                {policy: bootstrapv1alpha1.ConfigurationApplyStrategyReboot, want: ApplyModeReboot},
		"unset resolves to a controlled reboot": {want: ApplyModeReboot},
		"no reboot":                             {policy: bootstrapv1alpha1.ConfigurationApplyStrategyNoReboot, want: ApplyModeNoReboot},
		"unknown strategy":                      {policy: bootstrapv1alpha1.ConfigurationApplyStrategy("Whatever"), err: ErrPolicyUnknown},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveApplyMode(tt.policy)
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("ResolveApplyMode() error = %v, want %v", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveApplyMode() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ResolveApplyMode() = %q, want %q", got, tt.want)
			}
		})
	}
}
