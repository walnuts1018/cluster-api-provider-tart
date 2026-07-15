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

	machineexecutionhandler "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/handler"
	machineexecutionport "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/port"
)

type HostReferenceService = machineexecutionport.HostReferenceService
type ProvisionStep = machineexecutionport.ProvisionStep
type NodeHealthObserver = machineexecutionport.NodeHealthObserver

type Workflow struct {
	commands *machineexecutionhandler.CommandHandler
}

type StepExecutor struct {
	client.Client
	HostReferences HostReferenceService
	NodeHealth     NodeHealthObserver
	Provisioner    ProvisionStep
	Recorder       events.EventRecorder
}

func NewWorkflow(
	k8sClient client.Client,
	hostReferences HostReferenceService,
	nodeHealth NodeHealthObserver,
	provisioner ProvisionStep,
	recorder events.EventRecorder,
) *Workflow {
	return NewWorkflowWithSteps(NewStepExecutor(k8sClient, hostReferences, nodeHealth, provisioner, recorder))
}

func NewWorkflowWithSteps(steps *StepExecutor) *Workflow {
	return &Workflow{
		commands: machineexecutionhandler.NewCommandHandler(steps),
	}
}

func NewStepExecutor(
	k8sClient client.Client,
	hostReferences HostReferenceService,
	nodeHealth NodeHealthObserver,
	provisioner ProvisionStep,
	recorder events.EventRecorder,
) *StepExecutor {
	return &StepExecutor{
		Client:         k8sClient,
		HostReferences: hostReferences,
		NodeHealth:     nodeHealth,
		Provisioner:    provisioner,
		Recorder:       recorder,
	}
}
