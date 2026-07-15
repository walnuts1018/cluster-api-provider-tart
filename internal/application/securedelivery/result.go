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

package securedelivery

import "github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"

type Result interface {
	isResult()
}

type RegisterAccepted struct {
	Response agentprotocol.RegisterResponse
}

func (RegisterAccepted) isResult() {}

type RegisterRejected struct {
	Status  int
	Code    string
	Message string
}

func (RegisterRejected) isResult() {}

type BootstrapAccepted struct {
	Bundle agentprotocol.BootstrapBundle
}

func (BootstrapAccepted) isResult() {}

type BootstrapRejected struct {
	Status  int
	Code    string
	Message string
}

func (BootstrapRejected) isResult() {}
