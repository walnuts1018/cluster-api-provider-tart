//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
	"github.com/walnuts1018/cluster-api-provider-tart/test/e2e/framework"
)

// e2eNamespaceとe2eClusterNameは、本suiteが作成する全リソースの一貫した命名に使う。
// FreshProvision/InPlaceUpgrade/ReconcileRecoveryの3specは同一clusterを対象に順に実行される。
const (
	e2eNamespace   = "tart-e2e-workload"
	e2eClusterName = "tart-e2e-cluster"
	e2eHostName    = "tart-e2e-host-0"

	// e2eTalosVersion/e2eSchematicIDはlab上のTalos installで使うimageを固定する。
	// TODO: image factory schematicとTalos versionの組み合わせは、実CI実行時にlabのhardware
	// (QEMU virtio-scsi + virtio-net構成)向けの正しいschematic IDへ差し替える必要がある。
	e2eTalosVersion = "v1.14.0"
	e2eSchematicID  = "376567988ad370138ad8b2698212367b8edcb69b54a3628f6634457f479530d"

	e2eKubernetesVersion = "v1.34.0"
)

// freshProvisionSpecsは、FreshProvision specをginkgoのspec treeへ登録する。InPlaceUpgrade/
// ReconcileRecoveryはこのspecが構築した共有state(TartHost/Cluster)へ依存するため、suite_test.go
// の共通Ordered containerから宣言順(Fresh→InPlace→Reconcile)で呼び出される想定である
// (ginkgoは既定でtop-level containerの実行順をrandomizeするため、3つのDescribeを別々に
// トップレベル登録すると依存順序が保証されない)。
func freshProvisionSpecs() {
	Describe("FreshProvision", Ordered, func() {
		BeforeAll(func() {
			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				Name: e2eNamespace,
			})).To(Or(Succeed(), WithTransform(func(err error) bool {
				return err != nil && apierrors.IsAlreadyExists(err)
			}, BeTrue())))
		})

		It("registers a TartHost for the lab VM and observes hardware inventory via WoL+PXE+Talos maintenance discovery", func() {
			mac, err := network.ParseMACAddress(controlPlaneVMMACAddress)
			Expect(err).NotTo(HaveOccurred())
			broadcast, err := network.ParseUDPAddress(labBroadcastAddress())
			Expect(err).NotTo(HaveOccurred())

			host := &infrav1alpha1.TartHost{
				Name: e2eHostName,
				Spec: infrav1alpha1.TartHostSpec{
					MACAddress:    mac,
					Architecture:  "amd64",
					FailureDomain: "lab",
					Power: infrav1alpha1.PowerSpec{
						Backend: infrav1alpha1.PowerBackendWakeOnLAN,
						WakeOnLAN: &infrav1alpha1.WakeOnLANPowerConfig{
							BroadcastAddress: broadcast,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, host)).To(Succeed())

			By("waiting for InventoryReady=True (this exercises the real WoL power-on, PXE boot, and Talos maintenance discovery path)")
			framework.WaitForCondition(ctx, tartHostConditions(e2eHostName), infrav1alpha1.TartHostInventoryReadyCondition, metav1.ConditionTrue, 15*time.Minute)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: e2eHostName}, host)).To(Succeed())
			Expect(host.Status.Inventory).NotTo(BeNil())
			Expect(host.Status.Inventory.Disks).To(HaveLen(3), "expected system/ssd/hdd disks to be observed")
			for _, disk := range host.Status.Inventory.Disks {
				Expect(disk.StableSelector).NotTo(BeEmpty(), "disk %+v should have a unique stable selector", disk)
			}
		})

		It("provisions a single control-plane node via TartMachine/TartControlPlane/TartCluster using the observed StableSelector", func() {
			var host infrav1alpha1.TartHost
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: e2eHostName}, &host)).To(Succeed())

			systemDiskSelector := systemDiskStableSelector(host)
			Expect(systemDiskSelector).NotTo(BeEmpty())

			By("creating the immutable Secret-backed machine configuration patch input")
			patchesSecret := &corev1.Secret{
				Name: e2eClusterName + "-cp-patches", Namespace: e2eNamespace,
				Immutable: new(true),
				StringData: map[string]string{
					"patches": controlPlanePatches(systemDiskSelector),
				},
			}
			Expect(k8sClient.Create(ctx, patchesSecret)).To(Succeed())

			By("creating TartCluster/TartMachineTemplate/TartBootstrapConfigTemplate/TartControlPlane")
			tartCluster := &infrav1alpha1.TartCluster{
				Name: e2eClusterName, Namespace: e2eNamespace,
			}
			Expect(k8sClient.Create(ctx, tartCluster)).To(Succeed())

			machineTemplate := &infrav1alpha1.TartMachineTemplate{
				Name: e2eClusterName + "-cp", Namespace: e2eNamespace,
				Spec: infrav1alpha1.TartMachineTemplateSpec{
					Template: infrav1alpha1.TartMachineTemplateResource{
						Spec: infrav1alpha1.TartMachineTemplateResourceSpec{
							HostSelector: &infrav1alpha1.HostSelector{Architecture: "amd64"},
							Image: infrav1alpha1.TalosImageSpec{
								Version:     e2eTalosVersion,
								SchematicID: e2eSchematicID,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, machineTemplate)).To(Succeed())

			bootstrapTemplate := &bootstrapv1alpha1.TartBootstrapConfigTemplate{
				Name: e2eClusterName + "-cp", Namespace: e2eNamespace,
				Spec: bootstrapv1alpha1.TartBootstrapConfigTemplateSpec{
					Template: bootstrapv1alpha1.TartBootstrapConfigTemplateResource{
						Spec: bootstrapv1alpha1.TartBootstrapConfigTemplateResourceSpec{
							ConfigPatchesSecretRef: &corev1.LocalObjectReference{Name: patchesSecret.Name},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, bootstrapTemplate)).To(Succeed())

			replicas := int32(1)
			controlPlane := &controlplanev1alpha1.TartControlPlane{
				Name: e2eClusterName, Namespace: e2eNamespace,
				Spec: controlplanev1alpha1.TartControlPlaneSpec{
					Version:  e2eKubernetesVersion,
					Replicas: &replicas,
					MachineTemplate: controlplanev1alpha1.TartControlPlaneMachineTemplate{
						Spec: controlplanev1alpha1.TartControlPlaneMachineTemplateSpec{
							InfrastructureRef: clusterv1.ContractVersionedObjectReference{
								Kind:     "TartMachineTemplate",
								Name:     machineTemplate.Name,
								APIGroup: infrav1alpha1.GroupVersion.Group,
							},
						},
					},
					BootstrapConfigTemplateRef: clusterv1.ContractVersionedObjectReference{
						Kind:     "TartBootstrapConfigTemplate",
						Name:     bootstrapTemplate.Name,
						APIGroup: bootstrapv1alpha1.GroupVersion.Group,
					},
				},
			}
			Expect(k8sClient.Create(ctx, controlPlane)).To(Succeed())

			cluster := &clusterv1.Cluster{
				Name: e2eClusterName, Namespace: e2eNamespace,
				Spec: clusterv1.ClusterSpec{
					// TODO: 実CI実行時、workload cluster APIサーバへE2Eテストプロセスから到達できる
					// endpoint(lab networkのVM IP)を確定させてから設定する。現状はcontrolPlaneEndpoint
					// を空のままにし、TartClusterがReadyになるまでの待機挙動を確認する用途に留める。
					ControlPlaneRef: clusterv1.ContractVersionedObjectReference{
						Kind:     "TartControlPlane",
						Name:     controlPlane.Name,
						APIGroup: controlplanev1alpha1.GroupVersion.Group,
					},
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						Kind:     "TartCluster",
						Name:     tartCluster.Name,
						APIGroup: infrav1alpha1.GroupVersion.Group,
					},
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			By("waiting for TartCluster, TartControlPlane, Machine and Node to become Ready")
			framework.WaitForCondition(ctx, tartClusterConditions(e2eNamespace, e2eClusterName), infrav1alpha1.TartClusterReadyCondition, metav1.ConditionTrue, 20*time.Minute)
			framework.WaitForCondition(ctx, tartControlPlaneConditions(e2eNamespace, e2eClusterName), controlplanev1alpha1.TartControlPlaneAvailableCondition, metav1.ConditionTrue, 20*time.Minute)
			framework.WaitForCondition(ctx, machineConditionsForCluster(e2eNamespace, e2eClusterName), clusterv1.MachineNodeHealthyCondition, metav1.ConditionTrue, 20*time.Minute)
		})
	})
}

// tartHostConditionsは、指定名のcluster-scoped TartHostのstatus.conditionsを返すConditionGetterを作る。
func tartHostConditions(name string) framework.ConditionGetter {
	return func(ctx context.Context) ([]metav1.Condition, error) {
		var host infrav1alpha1.TartHost
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, &host); err != nil {
			return nil, fmt.Errorf("get TartHost %q: %w", name, err)
		}
		return host.Status.Conditions, nil
	}
}

func tartClusterConditions(namespace, name string) framework.ConditionGetter {
	return func(ctx context.Context) ([]metav1.Condition, error) {
		var cluster infrav1alpha1.TartCluster
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &cluster); err != nil {
			return nil, fmt.Errorf("get TartCluster %s/%s: %w", namespace, name, err)
		}
		return cluster.Status.Conditions, nil
	}
}

func tartControlPlaneConditions(namespace, name string) framework.ConditionGetter {
	return func(ctx context.Context) ([]metav1.Condition, error) {
		var controlPlane controlplanev1alpha1.TartControlPlane
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &controlPlane); err != nil {
			return nil, fmt.Errorf("get TartControlPlane %s/%s: %w", namespace, name, err)
		}
		return controlPlane.Status.Conditions, nil
	}
}

// machineConditionsForClusterは、clusterに属する最初のCAPI Machineのconditionsを返す。
// 本suiteは単一control-plane replicaしか扱わないため、複数Machineの曖昧さを気にしなくてよい。
func machineConditionsForCluster(namespace, clusterName string) framework.ConditionGetter {
	return func(ctx context.Context) ([]metav1.Condition, error) {
		var machines clusterv1.MachineList
		if err := k8sClient.List(ctx, &machines); // namespaceとcluster nameでの絞り込みだけで、本suiteの範囲では十分に一意である。
		err != nil {
			return nil, fmt.Errorf("list Machines: %w", err)
		}
		for i := range machines.Items {
			machine := &machines.Items[i]
			if machine.Namespace != namespace {
				continue
			}
			if machine.Spec.ClusterName != clusterName {
				continue
			}
			return machine.Status.Conditions, nil
		}
		return nil, fmt.Errorf("no Machine found yet for cluster %s/%s", namespace, clusterName)
	}
}

// systemDiskStableSelectorは、Host inventoryのdisk一覧からsystem disk(容量最大のdisk、
// このE2E labではSystemDiskGiB=40が最大)に対応するStableSelectorを返す。
// TODO: 容量だけによる推定は複数のdiskが同容量になる構成では機能しない。lab側でdiskサイズを
// 明確に分ける(現状は40/20/20 GiB)ことで一意性を担保しているが、より頑健にするには
// disk role(system/ssd/hdd)をserial命名規則(lab/diskimages.goのdiskSerial)から
// 直接読み取る方式への変更を検討する。
func systemDiskStableSelector(host infrav1alpha1.TartHost) string {
	if host.Status.Inventory == nil {
		return ""
	}
	var largest infrav1alpha1.DiskInventory
	for _, disk := range host.Status.Inventory.Disks {
		if disk.SizeBytes > largest.SizeBytes {
			largest = disk
		}
	}
	return largest.StableSelector
}

// controlPlanePatchesは、単一disk(systemDiskSelector)へのinstallを指定する最小限のTalos
// machine configuration patchをYAMLで返す。
func controlPlanePatches(systemDiskSelector string) string {
	return fmt.Sprintf(`apiVersion: v1alpha1
kind: InstallConfig
install:
  diskSelector:
    match: %s
`, systemDiskSelector)
}

// labBroadcastAddressは、lab network CIDR上のbroadcast address(host部が全1)にWoL標準port 9を
// 付与したUDP宛先を返す。wol-libvirt-gatewayはlibvirt domainのMACアドレスからVMを解決するため、
// broadcast先はgatewayが listenするlab network上のaddressである必要がある。
func labBroadcastAddress() string {
	// labNetworkCIDRは198.51.100.0/24であるため、broadcastは198.51.100.255固定でよい。
	return "198.51.100.255:9"
}
