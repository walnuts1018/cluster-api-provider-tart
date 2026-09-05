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

//go:generate go tool kessoku $GOFILE

package kessoku

import (
	kessokulib "github.com/mazrean/kessoku"
	"sigs.k8s.io/controller-runtime/pkg/client"

	controller "github.com/walnuts1018/cluster-api-provider-tart/controller"
)

// Reconcilers holds the reconcilers wired for cmd/controller-manager. As the policy
// packages (host, talos, bootstrap, controlplane, boot) grow real implementations in
// later sessions, their adapters will be added as additional kessoku providers here.
type Reconcilers struct {
	TartHost            *controller.TartHostReconciler
	TartCluster         *controller.TartClusterReconciler
	TartMachine         *controller.TartMachineReconciler
	TartBootstrapConfig *controller.TartBootstrapConfigReconciler
	TartControlPlane    *controller.TartControlPlaneReconciler
}

func provideTartHostReconciler(c client.Client) *controller.TartHostReconciler {
	return &controller.TartHostReconciler{Client: c}
}

func provideTartClusterReconciler(c client.Client) *controller.TartClusterReconciler {
	return &controller.TartClusterReconciler{Client: c}
}

func provideTartMachineReconciler(c client.Client) *controller.TartMachineReconciler {
	return &controller.TartMachineReconciler{Client: c}
}

func provideTartBootstrapConfigReconciler(c client.Client) *controller.TartBootstrapConfigReconciler {
	return &controller.TartBootstrapConfigReconciler{Client: c}
}

func provideTartControlPlaneReconciler(c client.Client) *controller.TartControlPlaneReconciler {
	return &controller.TartControlPlaneReconciler{Client: c}
}

func provideReconcilers(
	tartHost *controller.TartHostReconciler,
	tartCluster *controller.TartClusterReconciler,
	tartMachine *controller.TartMachineReconciler,
	tartBootstrapConfig *controller.TartBootstrapConfigReconciler,
	tartControlPlane *controller.TartControlPlaneReconciler,
) Reconcilers {
	return Reconcilers{
		TartHost:            tartHost,
		TartCluster:         tartCluster,
		TartMachine:         tartMachine,
		TartBootstrapConfig: tartBootstrapConfig,
		TartControlPlane:    tartControlPlane,
	}
}

var _ = kessokulib.Inject[Reconcilers](
	"InitializeReconcilers",
	kessokulib.Provide(provideTartHostReconciler),
	kessokulib.Provide(provideTartClusterReconciler),
	kessokulib.Provide(provideTartMachineReconciler),
	kessokulib.Provide(provideTartBootstrapConfigReconciler),
	kessokulib.Provide(provideTartControlPlaneReconciler),
	kessokulib.Provide(provideReconcilers),
)
