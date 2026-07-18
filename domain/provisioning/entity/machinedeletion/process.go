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

package machinedeletion

import (
	"fmt"

	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
)

type Observation interface {
	isObservation()
}

type Command interface {
	isCommand()
}

type ObservationHostReferenceAbsent struct{}
type ObservationHostReferenceLost struct{}
type ObservationHostReadyForCleaning struct{}
type ObservationCleaningOperationLost struct{}
type ObservationCleaningOperationRunning struct {
	Phase operationdomain.Phase
}
type ObservationCleaningOperationSucceeded struct{}
type ObservationCleaningOperationFailed struct {
	Phase operationdomain.Phase
}

type CommandReleaseFinalizer struct{}
type CommandStartCleaning struct{}
type CommandClearOperationReference struct{}
type CommandWaitCleaning struct{}
type CommandFailCleaning struct {
	Phase operationdomain.Phase
}

func (ObservationHostReferenceAbsent) isObservation()        {}
func (ObservationHostReferenceLost) isObservation()          {}
func (ObservationHostReadyForCleaning) isObservation()       {}
func (ObservationCleaningOperationLost) isObservation()      {}
func (ObservationCleaningOperationRunning) isObservation()   {}
func (ObservationCleaningOperationSucceeded) isObservation() {}
func (ObservationCleaningOperationFailed) isObservation()    {}

func (CommandReleaseFinalizer) isCommand()        {}
func (CommandStartCleaning) isCommand()           {}
func (CommandClearOperationReference) isCommand() {}
func (CommandWaitCleaning) isCommand()            {}
func (CommandFailCleaning) isCommand()            {}

func Decide(observation Observation) (Command, error) {
	switch observation := observation.(type) {
	case ObservationHostReferenceAbsent, ObservationHostReferenceLost:
		return CommandReleaseFinalizer{}, nil
	case ObservationHostReadyForCleaning:
		return CommandStartCleaning{}, nil
	case ObservationCleaningOperationLost:
		return CommandClearOperationReference{}, nil
	case ObservationCleaningOperationRunning:
		if observation.Phase.Terminal() {
			return nil, fmt.Errorf("running Cleaning operation cannot use terminal phase %q", observation.Phase)
		}
		return CommandWaitCleaning{}, nil
	case ObservationCleaningOperationSucceeded:
		return CommandReleaseFinalizer{}, nil
	case ObservationCleaningOperationFailed:
		if !observation.Phase.Terminal() {
			return nil, fmt.Errorf("failed Cleaning operation cannot use non-terminal phase %q", observation.Phase)
		}
		return CommandFailCleaning(observation), nil
	default:
		return nil, fmt.Errorf("unknown TartMachine deletion observation: %T", observation)
	}
}
