package host

import (
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

func TestProviderID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
		want string
		err  bool
	}{
		{name: "UUIDから決定論的に生成", id: "018f3c5e-5f8a-7c1b-9a2d-123456789abc", want: "tart://host/018f3c5e-5f8a-7c1b-9a2d-123456789abc"},
		{name: "空のIDは拒否", id: "", err: true},
		{name: "UUIDではないIDは拒否", id: "host-a", err: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.err {
				_, err := ProviderID(hostdomain.HostID{})
				if !errors.Is(err, ErrInvalidHostID) {
					t.Fatalf("ProviderID() error = %v, want ErrInvalidHostID", err)
				}
				return
			}
			id, parseErr := hostdomain.ParseHostID(tt.id)
			if parseErr != nil {
				t.Fatalf("ParseHostID() error = %v", parseErr)
			}
			got, err := ProviderID(id)
			if err != nil {
				t.Fatalf("ProviderID() error = %v", err)
			}
			if got.String() != tt.want {
				t.Errorf("ProviderID() = %q, want %q", got.String(), tt.want)
			}
		})
	}
}

func TestHasIdentityConflict(t *testing.T) {
	t.Parallel()

	base := infrav1alpha1.TartHost{
		Name: "host-a",
		UID:  types.UID("a"),
		Spec: infrav1alpha1.TartHostSpec{MACAddress: mustMACAddress(t, "00:00:5e:00:53:02")},
	}
	tests := []struct {
		name  string
		other infrav1alpha1.TartHost
		want  bool
	}{
		{name: "同じMACは競合", other: infrav1alpha1.TartHost{Name: "host-b", Spec: infrav1alpha1.TartHostSpec{MACAddress: mustMACAddress(t, "00-00-5E-00-53-02")}}, want: true},
		{name: "異なるMACは競合しない", other: infrav1alpha1.TartHost{Name: "host-b", Spec: infrav1alpha1.TartHostSpec{MACAddress: mustMACAddress(t, "00:00:5e:00:53:03")}}, want: false},
		{name: "空のMACは競合しない", other: infrav1alpha1.TartHost{Name: "host-b"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := HasIdentityConflict(base, []infrav1alpha1.TartHost{base, tt.other}); got != tt.want {
				t.Errorf("HasIdentityConflict() = %t, want %t", got, tt.want)
			}
		})
	}
}

func mustMACAddress(t *testing.T, value string) network.MACAddress {
	t.Helper()
	address, err := network.ParseMACAddress(value)
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}
	return address
}
