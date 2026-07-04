package wol

import (
	"context"
	"fmt"

	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
	wolpacket "github.com/walnuts1018/cluster-api-provider-tart/pkg/wol"
)

type Sender interface {
	Send(string) error
}

type Adapter struct {
	sender Sender
}

func New(sender Sender) *Adapter {
	return &Adapter{sender: sender}
}

func Default() *Adapter {
	sender := wolpacket.DefaultSender()
	return New(sender)
}

func (adapter *Adapter) PowerOn(
	ctx context.Context,
	target driverdomain.HostTarget,
	_ operationdomain.ID,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := adapter.sender.Send(target.BootMACAddress().String()); err != nil {
		return driverdomain.NewError(
			driverdomain.ErrorTemporary,
			fmt.Errorf("send Wake-on-LAN packet: %w", err),
		)
	}
	return nil
}
