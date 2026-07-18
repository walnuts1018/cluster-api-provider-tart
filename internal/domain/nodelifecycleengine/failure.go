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

package nodelifecycleengine

type Failure interface {
	isNodeLifecycleFailure()
}

type InvalidCurrentVersion struct {
	Value string
}

type InvalidTargetVersion struct {
	Value string
}

type UnsupportedLifecycleRuntime struct {
	Value string
}

type LifecycleRuntimeUnsupportedFailure struct {
	LifecycleRuntime LifecycleRuntime
	UpdateClass      UpdateClass
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

func (InvalidCurrentVersion) isNodeLifecycleFailure()              {}
func (InvalidTargetVersion) isNodeLifecycleFailure()               {}
func (UnsupportedLifecycleRuntime) isNodeLifecycleFailure()        {}
func (LifecycleRuntimeUnsupportedFailure) isNodeLifecycleFailure() {}
func (MajorVersionChangeUnsupported) isNodeLifecycleFailure()      {}
func (VersionDowngradeUnsupported) isNodeLifecycleFailure()        {}
func (MinorVersionSkipUnsupported) isNodeLifecycleFailure()        {}
func (WorkerControlPlaneOrderUnsatisfied) isNodeLifecycleFailure() {}
func (SnapshotRequired) isNodeLifecycleFailure()                   {}
func (StepNotInPlan) isNodeLifecycleFailure()                      {}
func (NodeNotReady) isNodeLifecycleFailure()                       {}
func (VersionMismatch) isNodeLifecycleFailure()                    {}
func (StaticPodsNotReady) isNodeLifecycleFailure()                 {}
func (EtcdQuorumLost) isNodeLifecycleFailure()                     {}
func (APIUnhealthy) isNodeLifecycleFailure()                       {}

func (NodeNotReady) isHealthGateFailure()       {}
func (VersionMismatch) isHealthGateFailure()    {}
func (StaticPodsNotReady) isHealthGateFailure() {}
func (EtcdQuorumLost) isHealthGateFailure()     {}
func (APIUnhealthy) isHealthGateFailure()       {}
