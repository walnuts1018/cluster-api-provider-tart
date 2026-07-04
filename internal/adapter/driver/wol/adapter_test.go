package wol

import (
	"testing"

	drivercontract "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/driver/contract"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/internal/application/driver"
)

func TestAdapterPowerOnContract(t *testing.T) {
	t.Parallel()

	sender := &recordingSender{}
	adapter := New(sender)
	drivercontract.PowerOn(t, adapter)

	if len(sender.addresses) != 2 {
		t.Fatalf("sent packets = %d, want 2", len(sender.addresses))
	}
	for _, address := range sender.addresses {
		if address != "00:00:5e:00:53:02" {
			t.Fatalf("sent address = %q, want normalized MAC", address)
		}
	}
}

func TestAdapterImplementsOnlyPowerOnPort(t *testing.T) {
	t.Parallel()

	adapter := any(New(&recordingSender{}))
	if _, ok := adapter.(applicationdriver.PowerOnDriver); !ok {
		t.Fatal("WoL adapter does not implement PowerOnDriver")
	}
	if _, ok := adapter.(applicationdriver.PowerOffDriver); ok {
		t.Fatal("WoL adapter unexpectedly implements PowerOffDriver")
	}
	if _, ok := adapter.(applicationdriver.PowerStateObserver); ok {
		t.Fatal("WoL adapter unexpectedly implements PowerStateObserver")
	}
	if _, ok := adapter.(applicationdriver.BootOverrideDriver); ok {
		t.Fatal("WoL adapter unexpectedly implements BootOverrideDriver")
	}
	if _, ok := adapter.(applicationdriver.VirtualMediaDriver); ok {
		t.Fatal("WoL adapter unexpectedly implements VirtualMediaDriver")
	}
}

type recordingSender struct {
	addresses []string
}

func (sender *recordingSender) Send(address string) error {
	sender.addresses = append(sender.addresses, address)
	return nil
}
