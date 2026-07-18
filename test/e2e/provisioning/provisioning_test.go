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
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
		clusterName string

		manager    *SimulatorManager
		simulators []*HostSimulator
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.TODO())
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
		macs := []string{"00:00:5e:00:53:00", "00:00:5e:00:53:01", "00:00:5e:00:53:02"}
		for i, mac := range macs {
			diskSerial := fmt.Sprintf("tartroot%d", i)
			host := &infrastructurev1beta1.TartHost{}
			host.Name = fmt.Sprintf("%s-host-%d", clusterName, i)
			host.Namespace = namespace.Name
			host.Labels = map[string]string{"tart.walnuts.dev/role": "worker"}
			if i == 0 {
				host.Labels["tart.walnuts.dev/role"] = "control-plane"
			}
			host.Spec.Identifiers.BootMACAddress = mac
			host.Spec.Architecture = infrastructurev1beta1.ArchitectureAMD64
			host.Spec.Firmware = infrastructurev1beta1.FirmwareUEFI
			host.Spec.PlatformProfile = "amd64-uefi-ab-ubuntu-24.04-kubeadm/v1"
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
		cleanupNodeReadySimulation(ctx, bootstrapClusterProxy.GetClient(), clusterName)
		if namespace != nil && !skipCleanup {
			framework.DeleteNamespace(ctx, framework.DeleteNamespaceInput{
				Deleter: bootstrapClusterProxy.GetClient(),
				Name:    namespace.Name,
			})
		}
		if watchCancel != nil {
			watchCancel()
		}
		cancel()
	})

	It("Should boot the Agent Artifact and register through the Agent API", func() {
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

		By("Waiting for the Agent to register and complete preflight")
		Eventually(func(g Gomega) string {
			matched, logText, err := simulators[0].LogContainsAll(
				"http://192.168.100.1:8082/ipxe",
				"/v1/agent-artifacts/sha256/",
				"/kernel... ok",
				"tart e2e provisioning-agent preflight starting",
				"Provisioning Agent preflight completed",
				"tart e2e provisioning-agent preflight completed",
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(matched).To(BeTrue())
			return logText
		}, 8*time.Minute, 2*time.Second).Should(ContainSubstring("tart e2e provisioning-agent preflight completed"))
	})

	It("Should complete provisioning after BootReport and workload Node Ready gates", func() {
		By("Applying a MachineDeployment template with fixed bootstrap data")
		workloadClusterTemplate := replacementClusterTemplate(namespace.Name, clusterName, e2eConfig.Variables["KUBERNETES_VERSION"])
		Expect(bootstrapClusterProxy.Create(ctx, workloadClusterTemplate, framework.CreateWithPolling(1*time.Minute, 250*time.Millisecond))).To(Succeed(), "Failed to apply the node-ready cluster template")

		By("Waiting for the Agent to register and the Provision operation to exist")
		waitForAgentPreflight(simulators[0])
		machine := waitForProvisioningTartMachine(ctx, bootstrapClusterProxy.GetClient(), namespace.Name)
		operation := waitForMachineOperation(ctx, bootstrapClusterProxy.GetClient(), machine)

		By("Preparing a reachable workload kubeconfig for the controller health observer")
		ensureWorkloadKubeconfigToManagementCluster(ctx, bootstrapClusterProxy, namespace.Name, clusterName)

		By("Publishing the simulated OS BootReport and workload Node Ready state")
		machineID := clusterName + "-machine-id"
		markProvisionOperationBootReported(ctx, bootstrapClusterProxy.GetClient(), operation, machineID)
		owner := waitForOwnerMachine(ctx, bootstrapClusterProxy.GetClient(), machine)
		nodeName := clusterName + "-node-ready"
		createReadyWorkloadNode(ctx, bootstrapClusterProxy.GetClient(), nodeName, machine.Spec.ProviderID, machineID, owner.Spec.Version)
		setOwnerMachineNodeRef(ctx, bootstrapClusterProxy.GetClient(), owner, nodeName)

		By("Waiting for TartMachine, Host, and Operation to converge to Provisioned")
		requeueTartMachine(ctx, bootstrapClusterProxy.GetClient(), machine)
		waitForTartMachineProvisioned(ctx, bootstrapClusterProxy.GetClient(), machine, owner.Spec.Version)
		waitForOperationPhase(ctx, bootstrapClusterProxy.GetClient(), operation, infrastructurev1beta1.TartHostOperationPhaseSucceeded)
		host := waitForHostByMAC(ctx, bootstrapClusterProxy.GetClient(), namespace.Name, simulators[0].macAddress)
		waitForHostPhase(ctx, bootstrapClusterProxy.GetClient(), host, infrastructurev1beta1.TartHostPhaseProvisioned)
	})

	It("Should replace a Machine through the default CAPI MachineDeployment path without Runtime Extension", func() {
		By("Confirming Runtime Extension is not registered")
		assertRuntimeExtensionDisabled(ctx, bootstrapClusterProxy.GetClient())

		By("Applying a MachineDeployment template with fixed bootstrap data")
		workloadClusterTemplate := replacementClusterTemplate(namespace.Name, clusterName, e2eConfig.Variables["KUBERNETES_VERSION"])
		Expect(bootstrapClusterProxy.Create(ctx, workloadClusterTemplate, framework.CreateWithPolling(1*time.Minute, 250*time.Millisecond))).To(Succeed(), "Failed to apply the replacement cluster template")

		By("Waiting for the initial MachineDeployment Machine Agent to register")
		waitForAgentPreflight(simulators[0])

		By("Scaling the worker MachineDeployment so CAPI creates a replacement candidate")
		originalWorker := waitForWorkerMachine(ctx, bootstrapClusterProxy.GetClient(), namespace.Name, "")
		scaleWorkerMachineDeployment(ctx, bootstrapClusterProxy.GetClient(), namespace.Name, clusterName, 2)

		By("Waiting for the replacement candidate Agent to register on another Host")
		replacementWorker := waitForWorkerMachine(ctx, bootstrapClusterProxy.GetClient(), namespace.Name, originalWorker.Name)
		Expect(replacementWorker.Name).NotTo(Equal(originalWorker.Name))
		waitForAgentPreflight(simulators[1])

		By("Deleting the original worker Machine after the replacement candidate exists")
		deleteWorkerMachine(ctx, bootstrapClusterProxy.GetClient(), originalWorker)
	})

	It("Should reallocate a retained or detached Host only after manual WipeAll completes", func() {
		By("Marking every Host retained or detached before creating the workload Machine")
		for i, sim := range simulators {
			host := waitForHostByMAC(ctx, bootstrapClusterProxy.GetClient(), namespace.Name, sim.macAddress)
			if i == 0 {
				markE2EHostRetained(ctx, bootstrapClusterProxy.GetClient(), host)
				continue
			}
			markE2EHostDetached(ctx, bootstrapClusterProxy.GetClient(), host)
		}

		By("Applying a MachineDeployment template while no Host is available")
		workloadClusterTemplate := replacementClusterTemplate(namespace.Name, clusterName, e2eConfig.Variables["KUBERNETES_VERSION"])
		Expect(bootstrapClusterProxy.Create(ctx, workloadClusterTemplate, framework.CreateWithPolling(1*time.Minute, 250*time.Millisecond))).To(Succeed(), "Failed to apply the replacement cluster template")

		By("Confirming retained or detached Hosts are not normal allocation candidates")
		machine := waitForSingleTartMachine(ctx, bootstrapClusterProxy.GetClient(), namespace.Name)
		confirmNoHostAllocation(ctx, bootstrapClusterProxy.GetClient(), machine)

		By("Creating a manual WipeAll operation for the retained Host")
		host := waitForHostByMAC(ctx, bootstrapClusterProxy.GetClient(), namespace.Name, simulators[0].macAddress)
		operation := createManualWipeAllOperation(ctx, bootstrapClusterProxy.GetClient(), host)
		waitForOperationPhase(ctx, bootstrapClusterProxy.GetClient(), operation, infrastructurev1beta1.TartHostOperationPhasePreparingBoot)
		waitForHostPhase(ctx, bootstrapClusterProxy.GetClient(), host, infrastructurev1beta1.TartHostPhaseCleaning)

		By("Completing the manual WipeAll operation through the controller workflow")
		markOperationAwaitingHealth(ctx, bootstrapClusterProxy.GetClient(), operation)
		waitForOperationPhase(ctx, bootstrapClusterProxy.GetClient(), operation, infrastructurev1beta1.TartHostOperationPhaseSucceeded)
		waitForHostPhase(ctx, bootstrapClusterProxy.GetClient(), host, infrastructurev1beta1.TartHostPhaseAvailable)

		By("Requeuing the pending TartMachine after the retained Host becomes reusable")
		requeueTartMachine(ctx, bootstrapClusterProxy.GetClient(), machine)
		waitForTartMachineHost(ctx, bootstrapClusterProxy.GetClient(), machine, host)
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

func markE2EHostRetained(ctx context.Context, client crclient.Client, host *infrastructurev1beta1.TartHost) {
	before := host.DeepCopy()
	host.Spec.ConsumerRef = nil
	Expect(client.Patch(ctx, host, crclient.MergeFrom(before))).To(Succeed())

	before = host.DeepCopy()
	host.Status.Phase = infrastructurev1beta1.TartHostPhaseRetained
	host.Status.LastStablePhase = infrastructurev1beta1.TartHostPhaseRetained
	apimeta.SetStatusCondition(&host.Status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionFalse,
		Reason:             "Retained",
		Message:            "Host data is retained and must be wiped before allocation",
		ObservedGeneration: host.Generation,
	})
	Expect(client.Status().Patch(ctx, host, crclient.MergeFrom(before))).To(Succeed())
}

func markE2EHostDetached(ctx context.Context, client crclient.Client, host *infrastructurev1beta1.TartHost) {
	before := host.DeepCopy()
	host.Spec.ConsumerRef = nil
	Expect(client.Patch(ctx, host, crclient.MergeFrom(before))).To(Succeed())

	before = host.DeepCopy()
	host.Status.Phase = infrastructurev1beta1.TartHostPhaseDetached
	host.Status.LastStablePhase = infrastructurev1beta1.TartHostPhaseDetached
	apimeta.SetStatusCondition(&host.Status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionFalse,
		Reason:             "Detached",
		Message:            "Host state is detached and must be wiped before allocation",
		ObservedGeneration: host.Generation,
	})
	Expect(client.Status().Patch(ctx, host, crclient.MergeFrom(before))).To(Succeed())
}

func assertRuntimeExtensionDisabled(ctx context.Context, client crclient.Client) {
	extension := &unstructured.Unstructured{}
	extension.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "runtime.cluster.x-k8s.io",
		Version: "v1beta2",
		Kind:    "ExtensionConfig",
	})
	err := client.Get(ctx, crclient.ObjectKey{
		Namespace: "cluster-api-provider-tart-system",
		Name:      "cluster-api-provider-tart",
	}, extension)
	Expect(apierrors.IsNotFound(err)).To(BeTrue(), "Provisioning E2E must exercise the default CAPI replacement path without ExtensionConfig")
}

func waitForAgentPreflight(sim *HostSimulator) {
	Eventually(func(g Gomega) string {
		matched, logText, err := sim.LogContainsAll(
			"tart e2e provisioning-agent preflight starting",
			"Provisioning Agent preflight completed",
			"tart e2e provisioning-agent preflight completed",
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(matched).To(BeTrue())
		return logText
	}, 8*time.Minute, 2*time.Second).Should(ContainSubstring("tart e2e provisioning-agent preflight completed"))
}

func waitForWorkerMachine(ctx context.Context, client crclient.Client, namespace, excludeName string) *clusterv1.Machine {
	var machine *clusterv1.Machine
	Eventually(func(g Gomega) {
		machines := &clusterv1.MachineList{}
		g.Expect(client.List(ctx, machines, crclient.InNamespace(namespace))).To(Succeed())
		for i := range machines.Items {
			candidate := &machines.Items[i]
			if candidate.Name == excludeName {
				continue
			}
			if strings.Contains(candidate.Spec.InfrastructureRef.Name, "-md-0") {
				machine = candidate.DeepCopy()
				return
			}
		}
		g.Expect(machine).NotTo(BeNil(), "worker Machine should exist")
	}, 5*time.Minute, 2*time.Second).Should(Succeed())
	return machine
}

func deleteWorkerMachine(ctx context.Context, client crclient.Client, machine *clusterv1.Machine) {
	Expect(client.Delete(ctx, machine)).To(Succeed())
}

func scaleWorkerMachineDeployment(ctx context.Context, client crclient.Client, namespace, clusterName string, replicas int64) {
	deployment := &unstructured.Unstructured{}
	deployment.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cluster.x-k8s.io",
		Version: "v1beta2",
		Kind:    "MachineDeployment",
	})
	Expect(client.Get(ctx, crclient.ObjectKey{
		Namespace: namespace,
		Name:      clusterName + "-md-0",
	}, deployment)).To(Succeed())

	before := deployment.DeepCopy()
	Expect(unstructured.SetNestedField(deployment.Object, replicas, "spec", "replicas")).To(Succeed())
	Expect(client.Patch(ctx, deployment, crclient.MergeFrom(before))).To(Succeed())
}

func waitForHostByMAC(ctx context.Context, client crclient.Client, namespace, macAddress string) *infrastructurev1beta1.TartHost {
	var host *infrastructurev1beta1.TartHost
	Eventually(func(g Gomega) {
		hosts := &infrastructurev1beta1.TartHostList{}
		g.Expect(client.List(ctx, hosts, crclient.InNamespace(namespace))).To(Succeed())
		for i := range hosts.Items {
			candidate := &hosts.Items[i]
			if candidate.Spec.Identifiers.BootMACAddress == macAddress {
				host = candidate.DeepCopy()
				return
			}
		}
		g.Expect(host).NotTo(BeNil(), "TartHost with MAC %s should exist", macAddress)
	}, time.Minute, time.Second).Should(Succeed())
	return host
}

func waitForSingleTartMachine(ctx context.Context, client crclient.Client, namespace string) *infrastructurev1beta1.TartMachine {
	var machine *infrastructurev1beta1.TartMachine
	Eventually(func(g Gomega) {
		machines := &infrastructurev1beta1.TartMachineList{}
		g.Expect(client.List(ctx, machines, crclient.InNamespace(namespace))).To(Succeed())
		g.Expect(machines.Items).To(HaveLen(1))
		machine = machines.Items[0].DeepCopy()
	}, 3*time.Minute, 2*time.Second).Should(Succeed())
	return machine
}

func confirmNoHostAllocation(ctx context.Context, client crclient.Client, machine *infrastructurev1beta1.TartMachine) {
	Consistently(func(g Gomega) {
		current := &infrastructurev1beta1.TartMachine{}
		g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(machine), current)).To(Succeed())
		g.Expect(current.Status.HostRef).To(BeNil())
	}, 20*time.Second, 2*time.Second).Should(Succeed())
}

func createManualWipeAllOperation(
	ctx context.Context,
	client crclient.Client,
	host *infrastructurev1beta1.TartHost,
) *infrastructurev1beta1.TartHostOperation {
	operationID, err := operationdomain.DeterministicID("manual-wipeall/" + string(host.UID))
	Expect(err).NotTo(HaveOccurred())
	operationName, err := operationdomain.ResourceName(string(host.UID))
	Expect(err).NotTo(HaveOccurred())
	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: host.Namespace,
			Name:      operationName,
		},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID:          operationID.String(),
			Type:                 infrastructurev1beta1.OperationTypeWipeAll,
			PlanDigest:           "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			DesiredObjectsDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: host.Namespace,
				Name:      host.Name,
				UID:       host.UID,
			},
			Deadline: metav1.NewTime(time.Now().Add(2 * time.Hour)),
		},
	}
	Expect(client.Create(ctx, operation)).To(Succeed())
	return operation
}

func waitForOperationPhase(
	ctx context.Context,
	client crclient.Client,
	operation *infrastructurev1beta1.TartHostOperation,
	phase infrastructurev1beta1.TartHostOperationPhase,
) {
	Eventually(func(g Gomega) {
		current := &infrastructurev1beta1.TartHostOperation{}
		g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(operation), current)).To(Succeed())
		g.Expect(current.Status.Phase).To(Equal(phase))
	}, 3*time.Minute, 2*time.Second).Should(Succeed())
}

func waitForHostPhase(
	ctx context.Context,
	client crclient.Client,
	host *infrastructurev1beta1.TartHost,
	phase infrastructurev1beta1.TartHostPhase,
) {
	Eventually(func(g Gomega) {
		current := &infrastructurev1beta1.TartHost{}
		g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(host), current)).To(Succeed())
		g.Expect(current.Status.Phase).To(Equal(phase))
	}, 3*time.Minute, 2*time.Second).Should(Succeed())
}

func markOperationAwaitingHealth(
	ctx context.Context,
	client crclient.Client,
	operation *infrastructurev1beta1.TartHostOperation,
) {
	current := &infrastructurev1beta1.TartHostOperation{}
	Expect(client.Get(ctx, crclient.ObjectKeyFromObject(operation), current)).To(Succeed())
	before := current.DeepCopy()
	current.Status.Phase = infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth
	Expect(client.Status().Patch(ctx, current, crclient.MergeFrom(before))).To(Succeed())
}

func requeueTartMachine(ctx context.Context, client crclient.Client, machine *infrastructurev1beta1.TartMachine) {
	current := &infrastructurev1beta1.TartMachine{}
	Expect(client.Get(ctx, crclient.ObjectKeyFromObject(machine), current)).To(Succeed())
	before := current.DeepCopy()
	if current.Annotations == nil {
		current.Annotations = map[string]string{}
	}
	current.Annotations["e2e.cluster-api-provider-tart.walnuts.dev/requeue-at"] = time.Now().UTC().Format(time.RFC3339Nano)
	Expect(client.Patch(ctx, current, crclient.MergeFrom(before))).To(Succeed())
}

func waitForTartMachineHost(
	ctx context.Context,
	client crclient.Client,
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
) {
	Eventually(func(g Gomega) {
		current := &infrastructurev1beta1.TartMachine{}
		g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(machine), current)).To(Succeed())
		g.Expect(current.Status.HostRef).NotTo(BeNil())
		g.Expect(current.Status.HostRef.UID).To(Equal(host.UID))
	}, 3*time.Minute, 2*time.Second).Should(Succeed())
}

func waitForProvisioningTartMachine(ctx context.Context, client crclient.Client, namespace string) *infrastructurev1beta1.TartMachine {
	var machine *infrastructurev1beta1.TartMachine
	Eventually(func(g Gomega) {
		machines := &infrastructurev1beta1.TartMachineList{}
		g.Expect(client.List(ctx, machines, crclient.InNamespace(namespace))).To(Succeed())
		g.Expect(machines.Items).To(HaveLen(1))
		current := machines.Items[0].DeepCopy()
		g.Expect(current.Spec.ProviderID).NotTo(BeEmpty())
		g.Expect(current.Status.OperationRef).NotTo(BeNil())
		g.Expect(current.Status.HostRef).NotTo(BeNil())
		machine = current.DeepCopy()
	}, 5*time.Minute, 2*time.Second).Should(Succeed())
	return machine
}

func waitForMachineOperation(
	ctx context.Context,
	client crclient.Client,
	machine *infrastructurev1beta1.TartMachine,
) *infrastructurev1beta1.TartHostOperation {
	var operation *infrastructurev1beta1.TartHostOperation
	Eventually(func(g Gomega) {
		currentMachine := &infrastructurev1beta1.TartMachine{}
		g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(machine), currentMachine)).To(Succeed())
		g.Expect(currentMachine.Status.OperationRef).NotTo(BeNil())
		currentOperation := &infrastructurev1beta1.TartHostOperation{}
		g.Expect(client.Get(ctx, crclient.ObjectKey{
			Namespace: currentMachine.Status.OperationRef.Namespace,
			Name:      currentMachine.Status.OperationRef.Name,
		}, currentOperation)).To(Succeed())
		g.Expect(currentOperation.Spec.Type).To(Equal(infrastructurev1beta1.OperationTypeProvision))
		operation = currentOperation.DeepCopy()
	}, 3*time.Minute, 2*time.Second).Should(Succeed())
	return operation
}

func markProvisionOperationBootReported(
	ctx context.Context,
	client crclient.Client,
	operation *infrastructurev1beta1.TartHostOperation,
	machineID string,
) {
	current := &infrastructurev1beta1.TartHostOperation{}
	Expect(client.Get(ctx, crclient.ObjectKeyFromObject(operation), current)).To(Succeed())
	before := current.DeepCopy()
	current.Status.Phase = infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth
	current.Status.LastBootReport = &infrastructurev1beta1.BootReportStatus{
		BootID:                 "e2e-boot-" + string(current.UID),
		MachineID:              machineID,
		ActiveSlot:             infrastructurev1beta1.OSSlotA,
		ArtifactGeneration:     1,
		StateMounted:           true,
		DataMounted:            true,
		BootstrapApplied:       true,
		BootstrapPayloadDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ReportedAt:             metav1.Now(),
	}
	Expect(client.Status().Patch(ctx, current, crclient.MergeFrom(before))).To(Succeed())
}

func waitForOwnerMachine(
	ctx context.Context,
	client crclient.Client,
	machine *infrastructurev1beta1.TartMachine,
) *clusterv1.Machine {
	var owner *clusterv1.Machine
	Eventually(func(g Gomega) {
		current := &infrastructurev1beta1.TartMachine{}
		g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(machine), current)).To(Succeed())
		found, err := util.GetOwnerMachine(ctx, client, current.ObjectMeta)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(found).NotTo(BeNil())
		owner = found.DeepCopy()
	}, 3*time.Minute, 2*time.Second).Should(Succeed())
	return owner
}

func setOwnerMachineNodeRef(ctx context.Context, client crclient.Client, machine *clusterv1.Machine, nodeName string) {
	current := &clusterv1.Machine{}
	Expect(client.Get(ctx, crclient.ObjectKeyFromObject(machine), current)).To(Succeed())
	before := current.DeepCopy()
	current.Status.NodeRef = clusterv1.MachineNodeReference{Name: nodeName}
	Expect(client.Status().Patch(ctx, current, crclient.MergeFrom(before))).To(Succeed())
}

func createReadyWorkloadNode(
	ctx context.Context,
	client crclient.Client,
	name string,
	providerID string,
	machineID string,
	kubernetesVersion string,
) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       corev1.NodeSpec{ProviderID: providerID},
	}
	err := client.Create(ctx, node)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}
	current := &corev1.Node{}
	Expect(client.Get(ctx, crclient.ObjectKey{Name: name}, current)).To(Succeed())
	before := current.DeepCopy()
	current.Spec.ProviderID = providerID
	Expect(client.Patch(ctx, current, crclient.MergeFrom(before))).To(Succeed())

	Expect(client.Get(ctx, crclient.ObjectKey{Name: name}, current)).To(Succeed())
	before = current.DeepCopy()
	current.Status.NodeInfo.MachineID = machineID
	current.Status.NodeInfo.KubeletVersion = kubernetesVersion
	current.Status.Conditions = []corev1.NodeCondition{{
		Type:               corev1.NodeReady,
		Status:             corev1.ConditionTrue,
		Reason:             "KubeletReady",
		Message:            "kubelet is posting ready status",
		LastHeartbeatTime:  metav1.Now(),
		LastTransitionTime: metav1.Now(),
	}}
	Expect(client.Status().Patch(ctx, current, crclient.MergeFrom(before))).To(Succeed())
}

func ensureWorkloadKubeconfigToManagementCluster(
	ctx context.Context,
	proxy framework.ClusterProxy,
	namespace string,
	clusterName string,
) {
	client := proxy.GetClient()
	serviceAccountName := clusterName + "-node-health"
	clusterRoleName := clusterName + "-node-health"
	bindingName := clusterName + "-node-health"

	serviceAccount := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
		Namespace: namespace,
		Name:      serviceAccountName,
	}}
	err := client.Create(ctx, serviceAccount)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}

	clusterRole := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName}}
	clusterRole.Rules = []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"nodes"},
		Verbs:     []string{"get", "list", "watch"},
	}}
	err = client.Create(ctx, clusterRole)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}

	binding := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: bindingName}}
	binding.RoleRef = rbacv1.RoleRef{
		APIGroup: rbacv1.GroupName,
		Kind:     "ClusterRole",
		Name:     clusterRoleName,
	}
	binding.Subjects = []rbacv1.Subject{{
		Kind:      rbacv1.ServiceAccountKind,
		Name:      serviceAccountName,
		Namespace: namespace,
	}}
	err = client.Create(ctx, binding)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}

	token, err := proxy.GetClientSet().CoreV1().ServiceAccounts(namespace).CreateToken(ctx, serviceAccountName, &authv1.TokenRequest{}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	rootCA := &corev1.ConfigMap{}
	Expect(client.Get(ctx, crclient.ObjectKey{Namespace: namespace, Name: "kube-root-ca.crt"}, rootCA)).To(Succeed())
	caData := rootCA.Data["ca.crt"]
	Expect(caData).NotTo(BeEmpty())

	kubeconfig := managementClusterKubeconfig(clusterName, token.Status.Token, []byte(caData))
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      clusterName + "-kubeconfig",
		},
		Type: clusterv1.ClusterSecretType,
		Data: map[string][]byte{"value": []byte(kubeconfig)},
	}
	err = client.Create(ctx, secret)
	if apierrors.IsAlreadyExists(err) {
		current := &corev1.Secret{}
		Expect(client.Get(ctx, crclient.ObjectKeyFromObject(secret), current)).To(Succeed())
		before := current.DeepCopy()
		current.Type = clusterv1.ClusterSecretType
		current.Data = secret.Data
		Expect(client.Patch(ctx, current, crclient.MergeFrom(before))).To(Succeed())
		return
	}
	Expect(err).NotTo(HaveOccurred())
}

func managementClusterKubeconfig(clusterName, token string, caData []byte) string {
	ca := base64.StdEncoding.EncodeToString(caData)
	return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: %[1]s
  cluster:
    server: https://kubernetes.default.svc
    certificate-authority-data: %[2]s
users:
- name: %[1]s-node-health
  user:
    token: %[3]s
contexts:
- name: %[1]s
  context:
    cluster: %[1]s
    user: %[1]s-node-health
current-context: %[1]s
`, clusterName, ca, token)
}

func waitForTartMachineProvisioned(
	ctx context.Context,
	client crclient.Client,
	machine *infrastructurev1beta1.TartMachine,
	kubernetesVersion string,
) {
	Eventually(func(g Gomega) {
		current := &infrastructurev1beta1.TartMachine{}
		g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(machine), current)).To(Succeed())
		g.Expect(current.Status.Initialization.Provisioned).NotTo(BeNil())
		g.Expect(*current.Status.Initialization.Provisioned).To(BeTrue())
		g.Expect(current.Status.InstalledDistributionVersion).To(Equal(kubernetesVersion))
		condition := apimeta.FindStatusCondition(current.Status.Conditions, "Ready")
		g.Expect(condition).NotTo(BeNil())
		g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		g.Expect(condition.Reason).To(Equal("Provisioned"))
	}, 5*time.Minute, 2*time.Second).Should(Succeed())
}

func cleanupNodeReadySimulation(ctx context.Context, client crclient.Client, clusterName string) {
	if clusterName == "" {
		return
	}
	objects := []crclient.Object{
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-node-ready"}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-node-health"}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-node-health"}},
	}
	for _, object := range objects {
		err := client.Delete(ctx, object)
		if err != nil && !apierrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	}
}

func replacementClusterTemplate(namespace, clusterName, kubernetesVersion string) []byte {
	return []byte(fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %[2]s-bootstrap
  namespace: %[1]s
stringData:
  format: cloud-config
  value: |
    #cloud-config
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: %[2]s
  namespace: %[1]s
  labels:
    cluster.x-k8s.io/cluster-name: %[2]s
spec:
  clusterNetwork:
    pods:
      cidrBlocks:
      - 192.168.0.0/16
    services:
      cidrBlocks:
      - 10.128.0.0/12
  infrastructureRef:
    apiGroup: infrastructure.cluster.x-k8s.io
    kind: TartCluster
    name: %[2]s
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TartCluster
metadata:
  name: %[2]s
  namespace: %[1]s
  labels:
    cluster.x-k8s.io/cluster-name: %[2]s
spec:
  artifactPolicy:
    allowedRegistries:
    - %[4]s
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: MachineDeployment
metadata:
  name: %[2]s-md-0
  namespace: %[1]s
spec:
  clusterName: %[2]s
  replicas: 1
  selector:
    matchLabels: null
  template:
    spec:
      clusterName: %[2]s
      version: %[3]s
      bootstrap:
        dataSecretName: %[2]s-bootstrap
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: TartMachineTemplate
        name: %[2]s-md-0
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TartMachineTemplate
metadata:
  name: %[2]s-md-0
  namespace: %[1]s
spec:
  template:
    spec:
      image:
        ref: "%[5]s"
      platformProfile: amd64-uefi-ab-ubuntu-24.04-kubeadm/v1
      updatePolicy:
        mode: Replace
      deletionPolicy: WipeAll
`, namespace, clusterName, kubernetesVersion, e2eConfig.Variables["OS_ARTIFACT_REGISTRY"], e2eConfig.Variables["OS_ARTIFACT_REF"]))
}
