package bootstrap

// MachineRoleはTalos machineが担うroleを表すprovider-neutralな値である。
// siderolabs machinery/config/machine.Typeへの変換はadapter/talos/configbuilderが行う。
type MachineRole int

const (
	MachineRoleControlPlane MachineRole = iota
	MachineRoleWorker
)

// Validはroleが既知の値であるかを返す。
func (r MachineRole) Valid() bool {
	return r == MachineRoleControlPlane || r == MachineRoleWorker
}

func (r MachineRole) String() string {
	switch r {
	case MachineRoleControlPlane:
		return "controlplane"
	case MachineRoleWorker:
		return "worker"
	default:
		return "unknown"
	}
}
