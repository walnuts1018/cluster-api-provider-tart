package tartbootstrapconfig

import (
	"errors"
	"strings"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
)

func TestClassifyConfigurationError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantReason string
		recognized bool
	}{
		{name: "identity conflict", err: errBootstrapIdentityConflict, wantReason: infrav1alpha1.ReasonIdentityConflict, recognized: true},
		{name: "wrapped configuration conflict", err: errors.Join(errors.New("render failed"), domainbootstrap.ErrConfigurationConflict), wantReason: "ConfigurationConflict", recognized: true},
		{name: "provider ID conflict", err: talos.ErrProviderIDConflict, wantReason: "ConfigurationConflict", recognized: true},
		{name: "ambiguous install disk", err: domainbootstrap.ErrDiskSelectionAmbiguous, wantReason: "InstallDiskUnavailable", recognized: true},
		{name: "invalid install configuration", err: domainbootstrap.ErrInstallConfigurationInvalid, wantReason: "InstallDiskUnavailable", recognized: true},
		{name: "unknown error retries", err: errors.New("temporary API failure"), wantReason: "ConfigurationInvalid", recognized: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reason, message, recognized := classifyConfigurationError(tt.err)
			if reason != tt.wantReason || recognized != tt.recognized || message == "" {
				t.Errorf("classifyConfigurationError() = reason %q, message %q, recognized %t", reason, message, recognized)
			}
		})
	}
}

func TestClassifyConfigurationErrorDoesNotExposeUnderlyingError(t *testing.T) {
	t.Parallel()

	secret := "private material that must not reach a Condition"
	_, message, recognized := classifyConfigurationError(errors.New(secret))
	if recognized {
		t.Fatal("classifyConfigurationError(unknown) recognized = true, want false")
	}
	if contains := strings.Contains(message, secret); contains {
		t.Fatalf("classifyConfigurationError() message contains underlying error: %q", message)
	}
}
