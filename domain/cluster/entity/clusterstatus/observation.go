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

package clusterstatus

type TartCluster struct {
	Generation        int64
	ControlPlaneReady bool
}

type CAPICluster interface {
	capiCluster()
}

type MissingClusterLabel struct{}

func (MissingClusterLabel) capiCluster() {}

type ClusterNotFound struct {
	Name string
}

func (ClusterNotFound) capiCluster() {}

type PausedCluster struct {
	Name string
}

func (PausedCluster) capiCluster() {}

type ActiveCluster struct {
	Name string
}

func (ActiveCluster) capiCluster() {}

type PauseObservation struct {
	SpecPaused      bool
	PausedAnnotated bool
}

func ObservePause(observation PauseObservation) CAPICluster {
	if observation.SpecPaused || observation.PausedAnnotated {
		return PausedCluster{}
	}
	return ActiveCluster{}
}
