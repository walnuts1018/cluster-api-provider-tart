package machinehealth

type Reason string

const (
	ReasonProvisioned        Reason = "Provisioned"
	ReasonNodeNotReady       Reason = "NodeNotReady"
	ReasonProviderIDMissing  Reason = "ProviderIDMissing"
	ReasonProviderIDMismatch Reason = "ProviderIDMismatch"
)

type NodeObservation struct {
	MachineProviderID string
	NodeProviderID    string
	NodeReady         bool
}

type Result struct {
	Ready   bool
	Reason  Reason
	Message string
}

func EvaluateNode(observation NodeObservation) Result {
	switch {
	case observation.MachineProviderID == "":
		return Result{
			Reason:  ReasonProviderIDMissing,
			Message: "TartMachine providerID is not set",
		}
	case observation.NodeProviderID == "":
		return Result{
			Reason:  ReasonProviderIDMissing,
			Message: "Workload Node providerID is not set",
		}
	case observation.MachineProviderID != observation.NodeProviderID:
		return Result{
			Reason:  ReasonProviderIDMismatch,
			Message: "TartMachine and workload Node providerIDs do not match",
		}
	case !observation.NodeReady:
		return Result{
			Reason:  ReasonNodeNotReady,
			Message: "Workload Node is not Ready",
		}
	case observation.NodeReady:
		return Result{
			Ready:   true,
			Reason:  ReasonProvisioned,
			Message: "Workload Node is Ready and providerID matches",
		}
	}
	return Result{
		Reason:  ReasonNodeNotReady,
		Message: "Workload Node health is unknown",
	}
}
