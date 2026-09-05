package contract

import (
	"context"
	"errors"
	"fmt"

	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/driver"
)

func PowerOn(implementation applicationdriver.PowerOnDriver) error {
	address, err := driverdomain.ParseMACAddress("00:00:5e:00:53:02")
	if err != nil {
		return fmt.Errorf("parse contract MAC address: %w", err)
	}
	operationID, err := operationdomain.ParseID("f4353748-c9ea-41c6-b321-94197b64330e")
	if err != nil {
		return fmt.Errorf("parse contract operation ID: %w", err)
	}
	target := driverdomain.NewHostTarget(address)

	if err := implementation.PowerOn(context.Background(), target, operationID); err != nil {
		return fmt.Errorf("first PowerOn: %w", err)
	}
	if err := implementation.PowerOn(context.Background(), target, operationID); err != nil {
		return fmt.Errorf("repeat PowerOn with the same operation ID: %w", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := implementation.PowerOn(cancelled, target, operationID); !errors.Is(err, context.Canceled) {
		return fmt.Errorf("PowerOn with cancelled context error = %w, want context.Canceled", err)
	}
	return nil
}
