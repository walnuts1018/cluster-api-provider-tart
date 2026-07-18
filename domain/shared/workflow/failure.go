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

package workflow

type Failure interface {
	isFailure()
	Message() string
}

type InvalidCommand struct {
	Detail string
}

func (InvalidCommand) isFailure() {}

func (failure InvalidCommand) Message() string {
	return failure.Detail
}

type DependencyFailure struct {
	Operation string
	Detail    string
}

func (DependencyFailure) isFailure() {}

func (failure DependencyFailure) Message() string {
	if failure.Operation == "" {
		return failure.Detail
	}
	return failure.Operation + ": " + failure.Detail
}

type InvariantViolation struct {
	Detail string
}

func (InvariantViolation) isFailure() {}

func (failure InvariantViolation) Message() string {
	return failure.Detail
}
