package bootreport

import (
	"errors"

	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

var (
	ErrUnexpectedPhase      = errors.New("boot report is not accepted in the current phase")
	ErrConflictingCompleted = errors.New("boot report conflicts with the completed boot trial")
)

type Report struct {
	BootID             string
	ActiveSlot         string
	ArtifactGeneration uint64
	StateMounted       bool
	DataMounted        bool
	BootstrapApplied   bool
}

type ExpectedBoot struct {
	ActiveSlot         string
	ArtifactGeneration uint64
}

type Decision string

const (
	DecisionRecorded  Decision = "Recorded"
	DecisionCompleted Decision = "Completed"
	DecisionDuplicate Decision = "Duplicate"
)

type Result struct {
	Decision  Decision
	NextPhase operationdomain.Phase
}

func Evaluate(
	phase operationdomain.Phase,
	current *Report,
	incoming Report,
	expected ExpectedBoot,
) (Result, error) {
	switch phase {
	case operationdomain.PhaseBootTrial:
		if current != nil && equal(*current, incoming) {
			return Result{Decision: DecisionDuplicate, NextPhase: phase}, nil
		}
		if bootCompleted(incoming, expected) {
			next, err := operationdomain.Transition(phase, operationdomain.PhaseAwaitingHealth)
			if err != nil {
				return Result{}, err
			}
			return Result{Decision: DecisionCompleted, NextPhase: next}, nil
		}
		return Result{Decision: DecisionRecorded, NextPhase: phase}, nil
	case operationdomain.PhaseAwaitingHealth:
		if current != nil && equal(*current, incoming) {
			return Result{Decision: DecisionDuplicate, NextPhase: phase}, nil
		}
		return Result{}, ErrConflictingCompleted
	case operationdomain.PhasePending,
		operationdomain.PhasePreparingBoot,
		operationdomain.PhaseWaitingForAgent,
		operationdomain.PhaseWriting,
		operationdomain.PhaseVerifying,
		operationdomain.PhaseDistributionUpdating,
		operationdomain.PhaseRollingBack,
		operationdomain.PhaseSucceeded,
		operationdomain.PhaseFailed,
		operationdomain.PhaseRecoveryRequired:
		return Result{}, ErrUnexpectedPhase
	case "":
		return Result{}, ErrUnexpectedPhase
	}
	return Result{}, ErrUnexpectedPhase
}

func bootCompleted(report Report, expected ExpectedBoot) bool {
	return report.ActiveSlot == expected.ActiveSlot &&
		report.ArtifactGeneration == expected.ArtifactGeneration &&
		report.StateMounted &&
		report.DataMounted &&
		report.BootstrapApplied
}

func equal(left, right Report) bool {
	return left == right
}
