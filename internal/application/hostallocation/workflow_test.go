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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/hostallocation"
)

func TestWorkflowReturnsFailurePresentationWhenNoHostMatches(t *testing.T) {
	t.Parallel()

	workflow := NewWorkflow(Ports{
		Candidates:   candidateReaderStub{},
		Reservations: &reservationWriterStub{},
	})

	result, err := workflow.Reconcile(t.Context(), ReconcileInput{Machine: testMachine()})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.Failure == nil || result.Failure.Reason != "NoAvailableHost" {
		t.Fatalf("Failure = %#v, want NoAvailableHost", result.Failure)
	}
}

func TestWorkflowRetriesSelectionOnReservationConflict(t *testing.T) {
	t.Parallel()

	candidate := testCandidate(t)
	host := &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      candidate.Host.Name,
			Namespace: candidate.Host.Namespace,
			UID:       types.UID(candidate.Host.UID),
		},
	}
	workflow := NewWorkflow(Ports{
		Candidates: candidateReaderStub{
			candidates: []domain.Candidate{candidate},
		},
		Reservations: &reservationWriterStub{
			results: []ReservationResult{RetrySelection{}, Reserved{Host: host}},
		},
	})

	result, err := workflow.Reconcile(t.Context(), ReconcileInput{Machine: testMachine()})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.Host == nil || result.Host.Name != host.Name {
		t.Fatalf("Host = %#v, want %s", result.Host, host.Name)
	}
}

func testMachine() *infrastructurev1beta1.TartMachine {
	return &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-a",
			Namespace: "default",
			UID:       types.UID("machine-a-uid"),
		},
		Spec: infrastructurev1beta1.TartMachineSpec{
			PlatformProfile: "amd64-uefi-ab-ubuntu-24.04-kubeadm/v1",
			HostSelector: infrastructurev1beta1.HostSelector{
				MatchLabels: map[string]string{"rack": "a"},
			},
		},
	}
}

func testCandidate(t *testing.T) domain.Candidate {
	t.Helper()

	set, err := capability.NewSet(capability.PowerOn)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	return domain.Candidate{
		Host: domain.HostRef{
			Namespace: "default",
			Name:      "host-a",
			UID:       "host-a-uid",
		},
		Phase:             hostdomain.PhaseAvailable,
		Assignment:        domain.Unassigned{},
		Architecture:      "amd64",
		Firmware:          "UEFI",
		PlatformProfile:   "amd64-uefi-ab-ubuntu-24.04-kubeadm/v1",
		RootDiskSizeBytes: 256_000_000_000,
		Capabilities:      set,
		Labels:            map[string]string{"rack": "a"},
	}
}

type candidateReaderStub struct {
	candidates []domain.Candidate
}

func (stub candidateReaderStub) ListCandidates(context.Context, *infrastructurev1beta1.TartMachine) ([]domain.Candidate, error) {
	return stub.candidates, nil
}

type reservationWriterStub struct {
	results []ReservationResult
	index   int
}

func (stub *reservationWriterStub) ReserveCandidate(context.Context, *infrastructurev1beta1.TartMachine, domain.HostRef) (ReservationResult, error) {
	if len(stub.results) == 0 {
		return RetrySelection{}, nil
	}
	result := stub.results[stub.index]
	if stub.index < len(stub.results)-1 {
		stub.index++
	}
	return result, nil
}
