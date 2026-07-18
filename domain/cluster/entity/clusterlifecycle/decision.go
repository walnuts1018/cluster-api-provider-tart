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

package clusterlifecycle

import "fmt"

type ObservedState interface {
	isObservedState()
}

type Command interface {
	isCommand()
}

type ObservedActive struct{}
type ObservedDeleting struct{}

type CommandReconcileActive struct{}
type CommandFinalizeDeleting struct{}

func (ObservedActive) isObservedState()   {}
func (ObservedDeleting) isObservedState() {}

func (CommandReconcileActive) isCommand()  {}
func (CommandFinalizeDeleting) isCommand() {}

func Decide(observed ObservedState) (Command, error) {
	switch observed.(type) {
	case ObservedActive:
		return CommandReconcileActive{}, nil
	case ObservedDeleting:
		return CommandFinalizeDeleting{}, nil
	default:
		return nil, fmt.Errorf("unknown TartCluster lifecycle state: %T", observed)
	}
}
