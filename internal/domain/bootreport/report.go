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
