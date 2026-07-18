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

package operation

type Result interface {
	isResult()
}

type InitializePending struct {
	Target Phase
	Events []Event
}

type PrepareBoot struct {
	Host   HostCommand
	Events []Event
}

type ActivateBoot struct {
	Events []Event
}

type ObserveActive struct {
	Events []Event
}

type AwaitMachineHealth struct {
	Events []Event
}

type CompleteOperation struct {
	Host   HostCommand
	Target Phase
	Events []Event
}

type HandleTerminal struct {
	Host   HostCommand
	Events []Event
}

type DeadlineExceeded struct {
	Outcome DeadlineOutcome
	Events  []Event
}

type Ignore struct {
	Events []Event
}

type Rejected struct {
	Failure Failure
	Events  []Event
}

type DeadlineOutcome interface {
	isDeadlineOutcome()
}

type DeadlineMarkFailed struct {
	WithUpdateFailure bool
	FailedPhase       Phase
}

type DeadlineRecordBootFailure struct{}

type DeadlineTransitionFailure struct {
	FailedPhase Phase
	Target      Phase
}

func (InitializePending) isResult()                  {}
func (PrepareBoot) isResult()                        {}
func (ActivateBoot) isResult()                       {}
func (ObserveActive) isResult()                      {}
func (AwaitMachineHealth) isResult()                 {}
func (CompleteOperation) isResult()                  {}
func (HandleTerminal) isResult()                     {}
func (DeadlineExceeded) isResult()                   {}
func (Ignore) isResult()                             {}
func (Rejected) isResult()                           {}
func (DeadlineMarkFailed) isDeadlineOutcome()        {}
func (DeadlineRecordBootFailure) isDeadlineOutcome() {}
func (DeadlineTransitionFailure) isDeadlineOutcome() {}
