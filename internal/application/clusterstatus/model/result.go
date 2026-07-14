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

package model

type Result interface {
	result()
}

type ResultSkippedMissingClusterLabel struct{}

func (ResultSkippedMissingClusterLabel) result() {}

type ResultSkippedClusterNotFound struct {
	ClusterName string
}

func (ResultSkippedClusterNotFound) result() {}

type ResultSkippedPausedCluster struct {
	ClusterName string
}

func (ResultSkippedPausedCluster) result() {}

type ResultUnchanged struct{}

func (ResultUnchanged) result() {}

type ResultPatched struct{}

func (ResultPatched) result() {}
