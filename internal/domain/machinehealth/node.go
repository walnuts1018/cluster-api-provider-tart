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

package machinehealth

type Reason string

const (
	ReasonProvisioned               Reason = "Provisioned"
	ReasonNodeNotReady              Reason = "NodeNotReady"
	ReasonProviderIDMissing         Reason = "ProviderIDMissing"
	ReasonProviderIDMismatch        Reason = "ProviderIDMismatch"
	ReasonMachineIDMismatch         Reason = "MachineIDMismatch"
	ReasonKubernetesVersionMismatch Reason = "KubernetesVersionMismatch"
)

type NodeObservation struct {
	MachineProviderID string
	NodeProviderID    string
	NodeReady         bool
	ExpectedMachineID string
	ObservedMachineID string
	ExpectedVersion   string
	NodeVersion       string
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
	case observation.ExpectedMachineID != "" &&
		(observation.ObservedMachineID == "" || observation.ObservedMachineID != observation.ExpectedMachineID):
		return Result{
			Reason:  ReasonMachineIDMismatch,
			Message: "Workload Node machine-id does not match the expected machine-id",
		}
	case !observation.NodeReady:
		return Result{
			Reason:  ReasonNodeNotReady,
			Message: "Workload Node is not Ready",
		}
	case observation.ExpectedVersion != "" && observation.NodeVersion != observation.ExpectedVersion:
		return Result{
			Reason:  ReasonKubernetesVersionMismatch,
			Message: "Workload Node Kubernetes version does not match the expected version",
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
