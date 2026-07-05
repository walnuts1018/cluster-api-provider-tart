// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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

func NewForAddress(address string) *Adapter {
	sender := wolpacket.NewSender(address)
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
