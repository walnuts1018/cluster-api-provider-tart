package fake

import (
	"testing"

	drivercontract "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/driver/contract"
)

func TestDriverPowerOnContract(t *testing.T) {
	t.Parallel()

	drivercontract.PowerOn(t, NewPowerOn())
}
