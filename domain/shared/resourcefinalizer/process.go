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

package resourcefinalizer

import "fmt"

type DesiredState interface {
	isDesiredState()
}

type ObservedState interface {
	isObservedState()
}

type Command interface {
	isCommand()
}

type DesiredPresent struct{}
type DesiredAbsent struct{}

type ObservedPresent struct{}
type ObservedAbsent struct{}

type CommandAdd struct{}
type CommandRemove struct{}
type CommandNoop struct{}

func (DesiredPresent) isDesiredState() {}
func (DesiredAbsent) isDesiredState()  {}

func (ObservedPresent) isObservedState() {}
func (ObservedAbsent) isObservedState()  {}

func (CommandAdd) isCommand()    {}
func (CommandRemove) isCommand() {}
func (CommandNoop) isCommand()   {}

func Decide(desired DesiredState, observed ObservedState) (Command, error) {
	switch desired.(type) {
	case DesiredPresent:
		switch observed.(type) {
		case ObservedPresent:
			return CommandNoop{}, nil
		case ObservedAbsent:
			return CommandAdd{}, nil
		default:
			return nil, fmt.Errorf("unknown resource finalizer observed state: %T", observed)
		}
	case DesiredAbsent:
		switch observed.(type) {
		case ObservedPresent:
			return CommandRemove{}, nil
		case ObservedAbsent:
			return CommandNoop{}, nil
		default:
			return nil, fmt.Errorf("unknown resource finalizer observed state: %T", observed)
		}
	default:
		return nil, fmt.Errorf("unknown resource finalizer desired state: %T", desired)
	}
}
