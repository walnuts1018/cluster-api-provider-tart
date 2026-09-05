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
