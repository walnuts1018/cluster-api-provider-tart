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

package machineexecution

import (
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Workflow struct {
	pipeline *pipeline
}

type workflowRuntime struct {
	client.Client
	HostReferences HostReferenceService
	NodeHealth     NodeHealthObserver
	Provisioner    Provisioner
	Recorder       events.EventRecorder
}

func NewWorkflow(
	k8sClient client.Client,
	hostReferences HostReferenceService,
	nodeHealth NodeHealthObserver,
	provisioner Provisioner,
	recorder events.EventRecorder,
) *Workflow {
	runtime := newWorkflowRuntime(k8sClient, hostReferences, nodeHealth, provisioner, recorder)
	return &Workflow{pipeline: newPipeline(runtime)}
}

func newWorkflowRuntime(
	k8sClient client.Client,
	hostReferences HostReferenceService,
	nodeHealth NodeHealthObserver,
	provisioner Provisioner,
	recorder events.EventRecorder,
) *workflowRuntime {
	return &workflowRuntime{
		Client:         k8sClient,
		HostReferences: hostReferences,
		NodeHealth:     nodeHealth,
		Provisioner:    provisioner,
		Recorder:       recorder,
	}
}
