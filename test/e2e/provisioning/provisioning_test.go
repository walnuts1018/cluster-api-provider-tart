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
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
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

	It("Should deliver the Agent boot script and Artifact kernel through PXE", func() {
		result.Cluster = &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace.Name,
			},
		}

		By("Applying the workload cluster template")
		workloadClusterTemplate := clusterctl.ConfigCluster(ctx, clusterctl.ConfigClusterInput{
			LogFolder:                filepath.Join(artifactsFolder, "clusters", bootstrapClusterProxy.GetName()),
			ClusterctlConfigPath:     clusterctlConfig,
			KubeconfigPath:           bootstrapClusterProxy.GetKubeconfigPath(),
			InfrastructureProvider:   e2eConfig.InfrastructureProviders()[0],
			Flavor:                   clusterctl.DefaultFlavor,
			Namespace:                namespace.Name,
			ClusterName:              clusterName,
			KubernetesVersion:        e2eConfig.Variables["KUBERNETES_VERSION"],
			ControlPlaneMachineCount: ptr.To[int64](1),
			WorkerMachineCount:       ptr.To[int64](0),
		})
		Expect(workloadClusterTemplate).NotTo(BeEmpty(), "Failed to get the cluster template")
		Expect(bootstrapClusterProxy.Create(ctx, workloadClusterTemplate, framework.CreateWithPolling(1*time.Minute, 250*time.Millisecond))).To(Succeed(), "Failed to apply the cluster template")

		By("Waiting for iPXE to fetch the Agent Artifact kernel")
		Eventually(func(g Gomega) string {
			matched, logText, err := simulators[0].LogContainsAll(
				"http://192.168.100.1:8082/ipxe",
				"/v1/agent-artifacts/sha256/",
				"/kernel... ok",
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(matched).To(BeTrue())
			return logText
		}, 8*time.Minute, 2*time.Second).Should(ContainSubstring("/kernel... ok"))
	})
})

func markE2EHostAvailable(ctx context.Context, client crclient.Client, host *infrastructurev1beta1.TartHost) {
	before := host.DeepCopy()
	host.Status.Phase = infrastructurev1beta1.TartHostPhaseAvailable
	host.Status.LastStablePhase = infrastructurev1beta1.TartHostPhaseAvailable
	host.Status.Capabilities = []infrastructurev1beta1.Capability{
		infrastructurev1beta1.CapabilityPowerOn,
	}
	host.Status.Inventory.RootDisk = infrastructurev1beta1.ObservedDisk{
		DeviceName:   host.Spec.RootDeviceHints.DeviceName,
		SerialNumber: host.Spec.RootDeviceHints.SerialNumber,
		SizeBytes:    host.Spec.RootDeviceHints.MinSizeBytes,
	}
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
