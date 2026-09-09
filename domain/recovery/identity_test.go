package recovery

import (
	"errors"
	"testing"

	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

func mustMAC(t *testing.T, value string) network.MACAddress {
	t.Helper()
	parsed, err := network.ParseMACAddress(value)
	if err != nil {
		t.Fatalf("ParseMACAddress(%q) error = %v", value, err)
	}
	return parsed
}

func mustClusterID(t *testing.T, value string) clusterdomain.ClusterID {
	t.Helper()
	parsed, err := clusterdomain.ParseClusterID(value)
	if err != nil {
		t.Fatalf("ParseClusterID(%q) error = %v", value, err)
	}
	return parsed
}

// TestVerifyResetTargetは、データを不可逆に破棄するResetの前提となるidentity照合がfail-closedであることを確認する。
func TestVerifyResetTarget(t *testing.T) {
	t.Parallel()

	const (
		otherIDValue = "0a5e2f4a-2c67-4d13-8f0e-7a1cbb5f7d92"
		systemUUID   = "1f3a9c22-7f1e-4b7a-9f0c-5d2e8a4b6c10"
		endpoint     = "198.51.100.10:50000"
	)
	clusterID := mustClusterID(t, "6b1b8e56-0a2c-4a5b-9c1f-1f2b7f0a9c31")
	otherID := mustClusterID(t, otherIDValue)
	mac := mustMAC(t, "00:00:5E:00:53:01")
	otherMAC := mustMAC(t, "00:00:5E:00:53:02")

	expected := ExpectedIdentity{ClusterID: clusterID, MACAddress: mac, SystemUUID: systemUUID, Endpoint: endpoint}
	observed := ObservedIdentity{ClusterID: clusterID, MACAddresses: []network.MACAddress{mac}, SystemUUID: systemUUID, Endpoint: endpoint}

	tests := []struct {
		name     string
		expected ExpectedIdentity
		observed ObservedIdentity
		wantErr  error
	}{
		{name: "matching identity is verified", expected: expected, observed: observed},
		{
			name:     "different cluster identity is rejected",
			expected: expected,
			observed: ObservedIdentity{ClusterID: otherID, MACAddresses: []network.MACAddress{mac}, SystemUUID: systemUUID, Endpoint: endpoint},
			wantErr:  ErrClusterIdentityMismatch,
		},
		{
			name:     "unknown cluster identity is rejected",
			expected: expected,
			observed: ObservedIdentity{MACAddresses: []network.MACAddress{mac}, SystemUUID: systemUUID, Endpoint: endpoint},
			wantErr:  ErrClusterIdentityMismatch,
		},
		{
			name:     "different MAC address is rejected even when the cluster matches",
			expected: expected,
			observed: ObservedIdentity{ClusterID: clusterID, MACAddresses: []network.MACAddress{otherMAC}, SystemUUID: systemUUID, Endpoint: endpoint},
			wantErr:  ErrHostIdentityMismatch,
		},
		{
			name:     "different system UUID is rejected even when MAC and cluster match",
			expected: expected,
			observed: ObservedIdentity{ClusterID: clusterID, MACAddresses: []network.MACAddress{mac}, SystemUUID: otherIDValue, Endpoint: endpoint},
			wantErr:  ErrHostIdentityMismatch,
		},
		{
			name:     "missing observed system UUID cannot confirm a recorded one",
			expected: expected,
			observed: ObservedIdentity{ClusterID: clusterID, MACAddresses: []network.MACAddress{mac}, Endpoint: endpoint},
			wantErr:  ErrHostIdentityUnverifiable,
		},
		{
			name:     "zero system UUID is treated as absent",
			expected: ExpectedIdentity{ClusterID: clusterID, MACAddress: mac, Endpoint: endpoint},
			observed: ObservedIdentity{ClusterID: clusterID, MACAddresses: []network.MACAddress{mac}, SystemUUID: "00000000-0000-0000-0000-000000000000", Endpoint: endpoint},
		},
		{
			name:     "different endpoint is rejected",
			expected: expected,
			observed: ObservedIdentity{ClusterID: clusterID, MACAddresses: []network.MACAddress{mac}, SystemUUID: systemUUID, Endpoint: "198.51.100.11:50000"},
			wantErr:  ErrEndpointUnknown,
		},
		{
			name:     "unknown Host enrollment MAC is rejected",
			expected: ExpectedIdentity{ClusterID: clusterID, Endpoint: endpoint},
			observed: observed,
			wantErr:  ErrHostIdentityMismatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := VerifyResetTarget(test.expected, test.observed)
			if test.wantErr == nil {
				if err != nil {
					t.Fatalf("VerifyResetTarget() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("VerifyResetTarget() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}
