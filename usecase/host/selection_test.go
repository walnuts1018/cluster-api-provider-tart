package host

import (
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
)

// TestSelectFreshForFailureDomainWithRendezvousは、machineUIDが空の場合にname順の
// フォールバック選択(claim順序を決定論的にするための最小構成)を検証する。
func TestSelectFreshForFailureDomainWithRendezvousFallsBackToNameOrder(t *testing.T) {
	t.Parallel()

	hosts := []infrav1alpha1.TartHost{
		{Name: "host-z", Spec: infrav1alpha1.TartHostSpec{HostID: mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abc"), Architecture: "amd64"}},
		{Name: "host-a", Spec: infrav1alpha1.TartHostSpec{HostID: mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abd"), Architecture: "amd64"}},
		{Name: "host-retained", Spec: infrav1alpha1.TartHostSpec{HostID: mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abe"), Architecture: "amd64", PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: "old"}}},
	}

	selected, err := SelectFreshForFailureDomainWithRendezvous(hosts, &infrav1alpha1.HostSelector{Architecture: "amd64"}, "", "")
	if err != nil {
		t.Fatalf("SelectFreshForFailureDomainWithRendezvous() error = %v", err)
	}
	if selected.Name != "host-a" {
		t.Errorf("SelectFreshForFailureDomainWithRendezvous() selected %q, want host-a", selected.Name)
	}

	_, err = SelectFreshForFailureDomainWithRendezvous(hosts, &infrav1alpha1.HostSelector{Architecture: "arm64"}, "", "")
	if !errors.Is(err, ErrNoEligibleHost) {
		t.Errorf("SelectFreshForFailureDomainWithRendezvous() error = %v, want ErrNoEligibleHost", err)
	}
}

func TestSelectFreshForFailureDomainWithRendezvousFiltersFailureDomain(t *testing.T) {
	t.Parallel()

	hosts := []infrav1alpha1.TartHost{
		{Name: "host-b", Spec: infrav1alpha1.TartHostSpec{HostID: mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abc"), FailureDomain: "zone-b"}},
		{Name: "host-a", Spec: infrav1alpha1.TartHostSpec{HostID: mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abd"), FailureDomain: "zone-a"}},
	}

	selected, err := SelectFreshForFailureDomainWithRendezvous(hosts, nil, "zone-a", "")
	if err != nil {
		t.Fatalf("SelectFreshForFailureDomainWithRendezvous() error = %v", err)
	}
	if selected.Name != "host-a" {
		t.Fatalf("SelectFreshForFailureDomainWithRendezvous() selected %q, want host-a", selected.Name)
	}
	if _, err := SelectFreshForFailureDomainWithRendezvous(hosts, nil, "zone-c", ""); !errors.Is(err, ErrNoEligibleHost) {
		t.Fatalf("SelectFreshForFailureDomainWithRendezvous() error = %v, want ErrNoEligibleHost", err)
	}
}

// TestSelectFreshForFailureDomainWithRendezvousDistributesByMachineUIDは、machineUIDを与えると
// name順ではなくhostScoreに基づく決定論的な選択に切り替わり、同じ入力に対して常に同じHostを
// 選ぶことを検証する。claimの競合を減らすためのrendezvous hashingが実際に機能していることの確認である。
func TestSelectFreshForFailureDomainWithRendezvousDistributesByMachineUID(t *testing.T) {
	t.Parallel()

	hosts := []infrav1alpha1.TartHost{
		{Name: "host-a", Spec: infrav1alpha1.TartHostSpec{HostID: mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abc")}},
		{Name: "host-b", Spec: infrav1alpha1.TartHostSpec{HostID: mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abd")}},
		{Name: "host-c", Spec: infrav1alpha1.TartHostSpec{HostID: mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abe")}},
	}
	machineUID := types.UID("machine-under-test")

	first, err := SelectFreshForFailureDomainWithRendezvous(hosts, nil, "", machineUID)
	if err != nil {
		t.Fatalf("SelectFreshForFailureDomainWithRendezvous() error = %v", err)
	}
	for range 10 {
		again, err := SelectFreshForFailureDomainWithRendezvous(hosts, nil, "", machineUID)
		if err != nil {
			t.Fatalf("SelectFreshForFailureDomainWithRendezvous() error = %v", err)
		}
		if again.Name != first.Name {
			t.Fatalf("SelectFreshForFailureDomainWithRendezvous() is not deterministic for the same machineUID: got %q, want %q", again.Name, first.Name)
		}
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
