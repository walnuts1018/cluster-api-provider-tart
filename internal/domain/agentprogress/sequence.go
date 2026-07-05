package agentprogress

import (
	"slices"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

type Decision string

const (
	DecisionApply     Decision = "Apply"
	DecisionDuplicate Decision = "Duplicate"
	DecisionGap       Decision = "Gap"
	DecisionInvalid   Decision = "Invalid"
)

type Progress struct {
	Step      string
	DiskRole  string
	Percent   int32
	Completed bool
}

type State struct {
	Sequence       int64
	Progress       *Progress
	CompletedSteps []string
}

type Evaluation struct {
	Decision Decision
	State    State
}

func NextPhase(current string, progress Progress) string {
	switch current {
	case "WaitingForAgent", "Writing", "Verifying":
	case "Pending", "PreparingBoot", "BootTrial", "AwaitingHealth",
		"DistributionUpdating", "RollingBack", "Succeeded", "Failed", "RecoveryRequired":
		return current
	default:
		return current
	}
	switch progress.Step {
	case agentprotocol.StepWriteImage:
		if progress.Completed {
			return "Verifying"
		}
		return "Writing"
	case agentprotocol.StepVerifyImage:
		return "Verifying"
	}
	return current
}

func Evaluate(saved State, sequence int64, incoming Progress) Evaluation {
	decision := evaluateSequence(saved.Sequence, sequence)
	if decision != DecisionApply {
		return Evaluation{Decision: decision, State: cloneState(saved)}
	}
	if incoming.Step == "" ||
		incoming.Percent < 0 ||
		incoming.Percent > 100 ||
		incoming.Percent%10 != 0 ||
		(incoming.Completed && incoming.Percent != 100) {
		return Evaluation{Decision: DecisionInvalid, State: cloneState(saved)}
	}
	if saved.Progress != nil &&
		saved.Progress.Step == incoming.Step &&
		saved.Progress.DiskRole == incoming.DiskRole &&
		incoming.Percent < saved.Progress.Percent {
		return Evaluation{Decision: DecisionInvalid, State: cloneState(saved)}
	}
	completedSteps := slices.Clone(saved.CompletedSteps)
	if incoming.Completed && !slices.Contains(completedSteps, incoming.Step) {
		completedSteps = append(completedSteps, incoming.Step)
	}
	progress := incoming
	return Evaluation{
		Decision: DecisionApply,
		State: State{
			Sequence:       sequence,
			Progress:       &progress,
			CompletedSteps: completedSteps,
		},
	}
}

func evaluateSequence(saved, incoming int64) Decision {
	switch {
	case saved < 0 || incoming <= 0:
		return DecisionInvalid
	case incoming <= saved:
		return DecisionDuplicate
	case incoming == saved+1:
		return DecisionApply
	case incoming > saved+1:
		return DecisionGap
	}
	return DecisionInvalid
}

func cloneState(state State) State {
	cloned := State{
		Sequence:       state.Sequence,
		CompletedSteps: slices.Clone(state.CompletedSteps),
	}
	if state.Progress != nil {
		progress := *state.Progress
		cloned.Progress = &progress
	}
	return cloned
}
