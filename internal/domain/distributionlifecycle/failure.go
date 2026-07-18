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

package distributionlifecycle

type Failure interface {
	isDistributionLifecycleFailure()
}

type InvalidCurrentVersion struct {
	Value string
}

type InvalidTargetVersion struct {
	Value string
}

type UnsupportedDistribution struct {
	Value string
}

type MajorVersionChangeUnsupported struct{}

type VersionDowngradeUnsupported struct{}

type MinorVersionSkipUnsupported struct{}

type WorkerControlPlaneOrderUnsatisfied struct {
	AcceptedVersion string
	TargetVersion   string
}

type SnapshotRequired struct {
	Step Step
}

type StepNotInPlan struct {
	Step Step
}

type HealthGateFailure interface {
	Failure
	isHealthGateFailure()
}

type NodeNotReady struct{}
type VersionMismatch struct {
	Current string
	Target  string
}
type StaticPodsNotReady struct{}
type EtcdQuorumLost struct{}
type APIUnhealthy struct{}

func (InvalidCurrentVersion) isDistributionLifecycleFailure()              {}
func (InvalidTargetVersion) isDistributionLifecycleFailure()               {}
func (UnsupportedDistribution) isDistributionLifecycleFailure()            {}
func (MajorVersionChangeUnsupported) isDistributionLifecycleFailure()      {}
func (VersionDowngradeUnsupported) isDistributionLifecycleFailure()        {}
func (MinorVersionSkipUnsupported) isDistributionLifecycleFailure()        {}
func (WorkerControlPlaneOrderUnsatisfied) isDistributionLifecycleFailure() {}
func (SnapshotRequired) isDistributionLifecycleFailure()                   {}
func (StepNotInPlan) isDistributionLifecycleFailure()                      {}
func (NodeNotReady) isDistributionLifecycleFailure()                       {}
func (VersionMismatch) isDistributionLifecycleFailure()                    {}
func (StaticPodsNotReady) isDistributionLifecycleFailure()                 {}
func (EtcdQuorumLost) isDistributionLifecycleFailure()                     {}
func (APIUnhealthy) isDistributionLifecycleFailure()                       {}

func (NodeNotReady) isHealthGateFailure()       {}
func (VersionMismatch) isHealthGateFailure()    {}
func (StaticPodsNotReady) isHealthGateFailure() {}
func (EtcdQuorumLost) isHealthGateFailure()     {}
func (APIUnhealthy) isHealthGateFailure()       {}
