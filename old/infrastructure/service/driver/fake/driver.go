package fake

import (
	"context"
	"sync"

	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
)

type PowerOnCall struct {
	Target      driverdomain.HostTarget
	OperationID operationdomain.ID
}

type Driver struct {
	mu            sync.Mutex
	powerOnErrors []error
	powerOnCalls  []PowerOnCall
}

func NewPowerOn(errors ...error) *Driver {
	return &Driver{powerOnErrors: append([]error(nil), errors...)}
}

func (driver *Driver) PowerOn(
	ctx context.Context,
	target driverdomain.HostTarget,
	operationID operationdomain.ID,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	driver.mu.Lock()
	defer driver.mu.Unlock()
	driver.powerOnCalls = append(driver.powerOnCalls, PowerOnCall{
		Target:      target,
		OperationID: operationID,
	})
	index := len(driver.powerOnCalls) - 1
	if index < len(driver.powerOnErrors) {
		return driver.powerOnErrors[index]
	}
	return nil
}

func (driver *Driver) PowerOnCalls() []PowerOnCall {
	driver.mu.Lock()
	defer driver.mu.Unlock()
	return append([]PowerOnCall(nil), driver.powerOnCalls...)
}
