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

// Package host contains the pure Host selection and allocation-eligibility policy, and
// the atomic compare-and-swap claim adapter for TartHost.spec.consumerRef. See
// .agents/skills/host-lifecycle/SKILL.md.
package host

import (
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// Eligibility is an observation of whether a Host may be selected for allocation. It is
// never stored as a workflow phase; it is always recomputed from spec.consumerRef,
// spec.retainedFrom, spec.reusePolicy, spec.reuseApproval and spec.reuseMode.
type Eligibility string

const (
	// Available Hosts have no consumerRef and no retainedFrom, so they may be claimed by
	// the deterministic Host allocator.
	Available Eligibility = "Available"
	// Claimed Hosts have a consumerRef and are bound to a specific TartMachine.
	Claimed Eligibility = "Claimed"
	// Retained Hosts held data or Talos identity from a previous Machine and must not be
	// auto-allocated until an explicit reuse approval matches the current retainedFrom.
	Retained Eligibility = "Retained"
	// Reusable Hosts are Retained Hosts with a matching reuse approval and reuse mode;
	// they may be selected for Adopt or Reprovision, but never for a normal claim path
	// that ignores reuseMode.
	Reusable Eligibility = "Reusable"
)

// Classify computes the current allocation eligibility of a Host from its Spec only.
// It never performs an external side effect and never treats reusePolicy/reuseApproval
// set while the Host is still fresh or Claimed as a future deletion approval: those
// fields only take effect once the Host is Retained and retainedFrom is present.
func Classify(spec infrav1alpha1.TartHostSpec) Eligibility {
	if spec.ConsumerRef != nil {
		return Claimed
	}
	if spec.RetainedFrom == nil {
		return Available
	}
	if spec.ReusePolicy == infrav1alpha1.ReusePolicyReusable &&
		spec.ReuseApproval != nil &&
		spec.RetainedFrom.UID != "" &&
		spec.ReuseApproval.RetainedFromUID != "" &&
		spec.ReuseApproval.RetainedFromUID == spec.RetainedFrom.UID &&
		validReuseMode(spec.ReuseMode) {
		return Reusable
	}
	return Retained
}

func validReuseMode(mode infrav1alpha1.ReuseMode) bool {
	return mode == infrav1alpha1.ReuseModeAdopt || mode == infrav1alpha1.ReuseModeReprovision
}
