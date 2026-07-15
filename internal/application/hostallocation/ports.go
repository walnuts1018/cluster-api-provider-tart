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

package hostallocation

import (
	"context"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/hostallocation"
)

type HostCandidateReader interface {
	ListCandidates(context.Context, *infrastructurev1beta1.TartMachine) ([]domain.Candidate, error)
}

type HostReservationWriter interface {
	ReserveCandidate(context.Context, *infrastructurev1beta1.TartMachine, domain.HostRef) (ReservationResult, error)
}

type Ports struct {
	Candidates   HostCandidateReader
	Reservations HostReservationWriter
}

// go-sumtype:decl ReservationResult
type ReservationResult interface {
	isReservationResult()
}

type Reserved struct {
	Host *infrastructurev1beta1.TartHost
}

type RetrySelection struct{}

func (Reserved) isReservationResult()       {}
func (RetrySelection) isReservationResult() {}
