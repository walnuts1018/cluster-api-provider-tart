//go:build e2e

/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package provisioning

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	"sigs.k8s.io/cluster-api/util"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Provisioning E2E tests", Label("Provisioning"), func() {
	var (
		namespace   *corev1.Namespace
		ctx         context.Context
		cancel      context.CancelFunc
		watchCancel context.CancelFunc
		result      *clusterctl.ApplyClusterTemplateAndWaitResult
		clusterName string

		manager    *SimulatorManager
		simulators []*HostSimulator
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.TODO())
		result = &clusterctl.ApplyClusterTemplateAndWaitResult{}
		clusterName = fmt.Sprintf("tart-e2e-%s", util.RandomString(6))

		Expect(bootstrapClusterProxy).NotTo(BeNil(), "BootstrapClusterProxy can't be nil")
		namespace, watchCancel = framework.CreateNamespaceAndWatchEvents(ctx, framework.CreateNamespaceAndWatchEventsInput{
			Creator:   bootstrapClusterProxy.GetClient(),
			ClientSet: bootstrapClusterProxy.GetClientSet(),
			Name:      fmt.Sprintf("tart-e2e-%s", util.RandomString(6)),
			LogFolder: filepath.Join(artifactsFolder, "clusters", bootstrapClusterProxy.GetName()),
		})

		By("Creating TartHosts and starting simulators")
		manager = NewSimulatorManager()
		macs := []string{"00:00:5e:00:53:00", "00:00:5e:00:53:01"}
		for i, mac := range macs {
			diskSerial := fmt.Sprintf("tartroot%d", i)
			host := &infrastructurev1beta1.TartHost{}
			host.Name = fmt.Sprintf("%s-host-%d", clusterName, i)
			host.Namespace = namespace.Name
			host.Spec.Identifiers.BootMACAddress = mac
			host.Spec.Architecture = infrastructurev1beta1.ArchitectureAMD64
			host.Spec.Firmware = infrastructurev1beta1.FirmwareUEFI
			host.Spec.PlatformProfile = "amd64-uefi-ab/v1"
			host.Spec.RootDeviceHints.DeviceName = fmt.Sprintf("/dev/disk/by-id/virtio-%s", diskSerial)
			host.Spec.RootDeviceHints.SerialNumber = diskSerial
			host.Spec.RootDeviceHints.MinSizeBytes = 64 * 1024 * 1024 * 1024
			host.Spec.Management.PowerDriver = "wol"
			host.Spec.Management.BootDriver = "ipxe"

			Expect(bootstrapClusterProxy.GetClient().Create(ctx, host)).To(Succeed())
			markE2EHostAvailable(ctx, bootstrapClusterProxy.GetClient(), host)

			sim, err := NewHostSimulator(mac, "br0", diskSerial)
			Expect(err).NotTo(HaveOccurred())
			simulators = append(simulators, sim)
			manager.Register(sim)
		}
		go func() {
			defer GinkgoRecover()
			Expect(manager.Start(ctx)).To(Succeed())
		}()
	})

	AfterEach(func() {
		for _, sim := range simulators {
			sim.Stop()
		}
		if result.Cluster != nil {
			framework.DumpSpecResourcesAndCleanup(ctx, clusterName, bootstrapClusterProxy, artifactsFolder, namespace.Name, namespace, watchCancel, result.Cluster, e2eConfig.GetIntervals, skipCleanup)
		}
		cancel()
	})

	It("Should provision a workload cluster", func() {
		cniURL := e2eConfig.Variables["CNI"]
		Expect(cniURL).NotTo(BeEmpty(), "CNI variable should be set in e2e config")

		By(fmt.Sprintf("Downloading CNI manifest from %s", cniURL))
		cniPath := filepath.Join(artifactsFolder, "cni.yaml")
		resp, err := http.Get(cniURL)
		Expect(err).NotTo(HaveOccurred(), "Failed to download CNI manifest")
		defer resp.Body.Close()

		cniFile, err := os.Create(cniPath)
		Expect(err).NotTo(HaveOccurred(), "Failed to create CNI manifest file")
		_, err = io.Copy(cniFile, resp.Body)
		Expect(err).NotTo(HaveOccurred(), "Failed to write CNI manifest file")
		cniFile.Close()

		clusterctl.ApplyClusterTemplateAndWait(ctx, clusterctl.ApplyClusterTemplateAndWaitInput{
			ClusterProxy:    bootstrapClusterProxy,
			CNIManifestPath: cniPath,
			ConfigCluster: clusterctl.ConfigClusterInput{
				LogFolder:                filepath.Join(artifactsFolder, "clusters", bootstrapClusterProxy.GetName()),
				ClusterctlConfigPath:     clusterctlConfig,
				KubeconfigPath:           bootstrapClusterProxy.GetKubeconfigPath(),
				InfrastructureProvider:   e2eConfig.InfrastructureProviders()[0],
				Flavor:                   clusterctl.DefaultFlavor,
				Namespace:                namespace.Name,
				ClusterName:              clusterName,
				KubernetesVersion:        e2eConfig.Variables["KUBERNETES_VERSION"],
				ControlPlaneMachineCount: ptr.To[int64](1),
				WorkerMachineCount:       ptr.To[int64](1),
			},
			WaitForClusterIntervals:      e2eConfig.GetIntervals(bootstrapClusterProxy.GetName(), "wait-cluster"),
			WaitForControlPlaneIntervals: e2eConfig.GetIntervals(bootstrapClusterProxy.GetName(), "wait-control-plane"),
			WaitForMachineDeployments:    e2eConfig.GetIntervals(bootstrapClusterProxy.GetName(), "wait-worker-nodes"),
		}, result)

		By("Waiting for the workload cluster nodes to be ready")
		workloadProxy := bootstrapClusterProxy.GetWorkloadCluster(ctx, namespace.Name, clusterName)
		framework.WaitForNodesReady(ctx, framework.WaitForNodesReadyInput{
			Lister:            workloadProxy.GetClient(),
			Count:             2, // 1 CP + 1 Worker
			WaitForNodesReady: e2eConfig.GetIntervals(bootstrapClusterProxy.GetName(), "wait-worker-nodes"),
		})
	})
})

func markE2EHostAvailable(ctx context.Context, client crclient.Client, host *infrastructurev1beta1.TartHost) {
	before := host.DeepCopy()
	host.Status.Phase = infrastructurev1beta1.TartHostPhaseAvailable
	host.Status.LastStablePhase = infrastructurev1beta1.TartHostPhaseAvailable
	host.Status.ObservedGeneration = host.Generation
	apimeta.SetStatusCondition(&host.Status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionTrue,
		Reason:             "Available",
		Message:            "Host is available for allocation",
		ObservedGeneration: host.Generation,
	})

	Expect(client.Status().Patch(ctx, host, crclient.MergeFrom(before))).To(Succeed())
}
