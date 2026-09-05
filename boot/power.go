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

// Package boot contains the minimal maintenance boot / power capability interface and
// its concrete backends (Wake-on-LAN, Redfish, manual). Power and boot are treated as a
// capability of a TartHost, not a fixed DHCP/TFTP/PXE implementation. See
// .agents/skills/host-lifecycle/SKILL.md.
package boot

import "context"

// PowerOn requests that a Host power on. Success only means the request was accepted;
// it does not imply maintenance Talos has started or that installation succeeded.
// Callers must observe the maintenance/authenticated Talos API separately before
// treating a Host as provisioned.
type PowerOn interface {
	PowerOn(ctx context.Context) error
}
