package host

import (
	"errors"
	"testing"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
)

func TestSelectFresh(t *testing.T) {
	t.Parallel()

	hosts := []infrav1alpha1.TartHost{
		{Name: "host-z", Spec: infrav1alpha1.TartHostSpec{HostID: infrav1alpha1.HostID(mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abc").String()), Architecture: "amd64"}},
		{Name: "host-a", Spec: infrav1alpha1.TartHostSpec{HostID: infrav1alpha1.HostID(mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abd").String()), Architecture: "amd64"}},
		{Name: "host-retained", Spec: infrav1alpha1.TartHostSpec{HostID: infrav1alpha1.HostID(mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abe").String()), Architecture: "amd64", PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: "old"}}},
	}

	selected, err := SelectFresh(hosts, &infrav1alpha1.HostSelector{Architecture: "amd64"})
	if err != nil {
		t.Fatalf("SelectFresh() error = %v", err)
	}
	if selected.Name != "host-a" {
		t.Errorf("SelectFresh() selected %q, want host-a", selected.Name)
	}

	_, err = SelectFresh(hosts, &infrav1alpha1.HostSelector{Architecture: "arm64"})
	if !errors.Is(err, ErrNoEligibleHost) {
		t.Errorf("SelectFresh() error = %v, want ErrNoEligibleHost", err)
	}
}

func mustHostID(t *testing.T, value string) hostdomain.HostID {
	t.Helper()
	id, err := hostdomain.ParseHostID(value)
	if err != nil {
		t.Fatalf("ParseHostID() error = %v", err)
	}
	return id
}
