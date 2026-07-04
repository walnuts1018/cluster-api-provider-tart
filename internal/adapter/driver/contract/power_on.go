package contract

import (
	"context"
	"testing"

	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/internal/application/driver"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

func PowerOn(t *testing.T, implementation applicationdriver.PowerOnDriver) {
	t.Helper()

	address, err := driverdomain.ParseMACAddress("00:00:5e:00:53:02")
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}
	operationID, err := operationdomain.ParseID("f4353748-c9ea-41c6-b321-94197b64330e")
	if err != nil {
		t.Fatalf("ParseID() error = %v", err)
	}
	target := driverdomain.NewHostTarget(address)

	if err := implementation.PowerOn(context.Background(), target, operationID); err != nil {
		t.Fatalf("first PowerOn() error = %v", err)
	}
	if err := implementation.PowerOn(context.Background(), target, operationID); err != nil {
		t.Fatalf("repeated PowerOn() error = %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := implementation.PowerOn(cancelled, target, operationID); err == nil {
		t.Fatal("PowerOn() with cancelled context error = nil")
	}
}
