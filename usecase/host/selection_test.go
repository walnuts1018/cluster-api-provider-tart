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
		{Name: "host-z", Spec: infrav1alpha1.TartHostSpec{HostID: mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abc").String(), Architecture: "amd64"}},
		{Name: "host-a", Spec: infrav1alpha1.TartHostSpec{HostID: mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abd").String(), Architecture: "amd64"}},
		{Name: "host-retained", Spec: infrav1alpha1.TartHostSpec{HostID: mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abe").String(), Architecture: "amd64", PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: "old"}}},
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

func TestSelectFreshForFailureDomain(t *testing.T) {
	t.Parallel()

	hosts := []infrav1alpha1.TartHost{
		{Name: "host-b", Spec: infrav1alpha1.TartHostSpec{HostID: mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abc").String(), FailureDomain: "zone-b"}},
		{Name: "host-a", Spec: infrav1alpha1.TartHostSpec{HostID: mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abd").String(), FailureDomain: "zone-a"}},
	}

	selected, err := SelectFreshForFailureDomain(hosts, nil, "zone-a")
	if err != nil {
		t.Fatalf("SelectFreshForFailureDomain() error = %v", err)
	}
	if selected.Name != "host-a" {
		t.Fatalf("SelectFreshForFailureDomain() selected %q, want host-a", selected.Name)
	}
	if _, err := SelectFreshForFailureDomain(hosts, nil, "zone-c"); !errors.Is(err, ErrNoEligibleHost) {
		t.Fatalf("SelectFreshForFailureDomain() error = %v, want ErrNoEligibleHost", err)
	}
}

func TestFailureDomains(t *testing.T) {
	t.Parallel()

	hosts := []infrav1alpha1.TartHost{
		{Spec: infrav1alpha1.TartHostSpec{FailureDomain: "zone-b"}},
		{Spec: infrav1alpha1.TartHostSpec{FailureDomain: "zone-a"}},
		{Spec: infrav1alpha1.TartHostSpec{FailureDomain: "zone-b"}},
		{Spec: infrav1alpha1.TartHostSpec{}},
	}

	got := FailureDomains(hosts)
	if len(got) != 2 || got[0].Name != "zone-a" || got[1].Name != "zone-b" {
		t.Fatalf("FailureDomains() = %#v, want sorted unique domains", got)
	}
	if got[0].ControlPlane == nil || !*got[0].ControlPlane || got[1].ControlPlane == nil || !*got[1].ControlPlane {
		t.Fatal("FailureDomains() must mark observed domains as control-plane capable")
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
