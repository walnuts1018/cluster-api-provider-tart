package update

import (
	"errors"
	"testing"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
)

func TestResolveApplyMode(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		policy bootstrapv1alpha1.ConfigurationUpdatePolicy
		want   ApplyMode
		err    error
	}{
		// Talos 1.14はreboot要否を信頼できる形で判定できないため、Autoは楽観的なreboot-free applyを試みない。
		"auto resolves to a controlled reboot":  {policy: bootstrapv1alpha1.ConfigurationUpdatePolicyAuto, want: ApplyModeReboot},
		"unset resolves to a controlled reboot": {want: ApplyModeReboot},
		"live":                                  {policy: bootstrapv1alpha1.ConfigurationUpdatePolicyLive, want: ApplyModeLive},
		"reboot":                                {policy: bootstrapv1alpha1.ConfigurationUpdatePolicyReboot, want: ApplyModeReboot},
		"initial only is not applicable":        {policy: bootstrapv1alpha1.ConfigurationUpdatePolicyInitialOnly, err: ErrPolicyUnknown},
		"unknown policy":                        {policy: bootstrapv1alpha1.ConfigurationUpdatePolicy("Whatever"), err: ErrPolicyUnknown},
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
