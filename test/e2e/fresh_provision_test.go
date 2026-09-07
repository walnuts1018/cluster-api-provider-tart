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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
	"github.com/walnuts1018/cluster-api-provider-tart/test/e2e/framework"
	"github.com/walnuts1018/cluster-api-provider-tart/test/e2e/lab"
)

// e2eNamespaceとe2eClusterNameは、本suiteが作成する全リソースの一貫した命名に使う。
// FreshProvision/InPlaceUpgrade/ReconcileRecoveryの3specは同一clusterを対象に順に実行される。
const (
	e2eNamespace      = "tart-e2e-workload"
	e2eClusterName    = "tart-e2e-cluster"
	e2eHostName       = "tart-e2e-host-0"
	e2eDataVolumeName = "tart-e2e-data"

	// e2eTalosVersion/e2eSchematicIDはlab上のTalos installで使うimageを固定する。
	// e2eSchematicIDは"customization: {}"(カスタマイズ無し)に対応する、Talos Image Factoryの
	// 実際のAPI(POST https://factory.talos.dev/schematics)から得られる値である。
	// QEMU virtio-scsi/virtio-net構成はTalosの標準extension setで十分動作するため、
	// system extensionのカスタマイズは不要。
	e2eTalosVersion = "v1.14.0"
	e2eSchematicID  = "376567988ad370138ad8b2698212367b8edcb69b5fd68c80be1f2ec7d603b4ba"

	e2eKubernetesVersion = "v1.34.0"
)

const e2eDataVolumeHostPath = "/var/mnt/" + e2eDataVolumeName

// clusterProvisioningTerminalReasonsは、TartCluster/TartControlPlane/MachineのConditionが
// これらのReasonを報告し続けている場合、reconcilerが外部からの承認や入力なしには自己解決
// できないと判断し、20分のtimeoutを待たずfail-fastするためのReason集合である。
// TalosUnreachable/MaintenanceUnavailable等、正常な起動シーケンスの途中で一時的に現れうる
// Reasonは、恒久障害と誤認してfail-fastが誤発火しないよう意図的に含めない。
var clusterProvisioningTerminalReasons = framework.TerminalReasons{
	infrav1alpha1.ReasonIdentityConflict:         "duplicated stable Host identity requires manual resolution",
	infrav1alpha1.ReasonDiskIdentityConflict:     "duplicated disk identity requires manual resolution",
	infrav1alpha1.ReasonUnsafeUpdate:             "an in-place update was judged unsafe and stopped fail-closed",
	infrav1alpha1.ReasonHostMismatch:             "the allocated Host does not match the Machine's placement constraints",
	infrav1alpha1.ReasonNoEligibleHost:           "no eligible fresh Host is available",
	infrav1alpha1.ReasonDeletionApprovalRequired: "the Host requires an explicit deletion approval",
	infrav1alpha1.ReasonReuseApprovalRequired:    "the Host requires an explicit reuse approval",
	infrav1alpha1.ReasonRolledBack:               "the previously observed Talos image is no longer running",
	infrav1alpha1.ReasonNotImplemented:           "the required behavior is not implemented by this provider",
}

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
			// TartHostはinventory観測前にTalos maintenance APIへ接続するendpointを必要とするが、
			// 初回はinventoryが無いためStatus.Addressesからも導出できない(鶏卵問題)。
			// lab networkのDHCP static reservationで固定したIPを明示指定して解決する。
			talosAPIAddress, err := network.ParseEndpoint(controlPlaneVMStaticIP)
			Expect(err).NotTo(HaveOccurred())

			host := &infrav1alpha1.TartHost{
				Name: e2eHostName,
				Spec: infrav1alpha1.TartHostSpec{
					MACAddress:      mac,
					TalosAPIAddress: talosAPIAddress,
					Architecture:    "amd64",
					FailureDomain:   "lab",
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
			// Talosのhardware discoveryは、Talos自身のsquashfs用loop device(/dev/loop*、
			// transport無し)も含めて全block deviceを正しく報告する。labが用意した3disk
			// (system/ssd/hdd)はvirtio transportのSCSI diskとしてのみ現れるため、それだけを
			// 抽出して検証する。
			var physicalDisks []infrav1alpha1.DiskInventory
			for _, disk := range host.Status.Inventory.Disks {
				if disk.Transport == "" {
					continue
				}
				physicalDisks = append(physicalDisks, disk)
			}
			Expect(physicalDisks).To(HaveLen(3), "expected system/ssd/hdd disks to be observed")
			for _, role := range []string{"system", "ssd", "hdd"} {
				wwid := labDiskWWID(role)
				matches := make([]infrav1alpha1.DiskInventory, 0, 1)
				for _, disk := range physicalDisks {
					if disk.WWID == wwid {
						matches = append(matches, disk)
					}
				}
				Expect(matches).To(HaveLen(1), "expected exactly one %s disk with wwid %q", role, wwid)
				Expect(matches[0].StableSelector).NotTo(BeEmpty(), "disk with wwid %q should have a stable selector", wwid)
			}
		})

		It("provisions a single control-plane node via TartMachine/TartControlPlane/TartCluster using the observed StableSelector", func() {
			var host infrav1alpha1.TartHost
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: e2eHostName}, &host)).To(Succeed())

			// このlabは3disk(system/ssd/hdd)を持ち、writableなdisk候補が複数存在するため、
			// domainbootstrap.SelectDiskは自動選択をfail-closedで拒否する
			// (reason=InstallDiskAmbiguous)。この場合はusecase/bootstrap.
			// MachineConfigurationContext.InstallDiskがnilのまま render され、
			// raw patchがinstall targetを明示しなければならない設計になっているため、
			// 観測したStableSelector(CEL式)をUnattendedInstallConfig documentとして
			// 明示的に渡す。CEL式は`!`始まりの値がYAML tag directiveと誤解釈される
			// 事故を避けるため、必ず%qで二重引用符化してから埋め込む。
			systemDiskSelector := systemDiskStableSelector(host)
			Expect(systemDiskSelector).NotTo(BeEmpty())

			By("creating the immutable Secret-backed machine configuration patch input")
			dataDiskSelector := fmt.Sprintf(`disk.wwid == %q`, labDiskWWID("hdd"))
			patches := fmt.Sprintf(`cluster: {}
---
apiVersion: v1alpha1
kind: UnattendedInstallConfig
provisioning:
  diskSelector:
    match: %q
  wipe: false
---
apiVersion: v1alpha1
kind: UserVolumeConfig
name: %s
volumeType: disk
provisioning:
  diskSelector:
    match: %q
  minSize: 1GiB
  maxSize: 10GiB
filesystem:
  type: ext4
`, systemDiskSelector, e2eDataVolumeName, dataDiskSelector)
			patchesSecret := &corev1.Secret{
				Name: e2eClusterName + "-cp-patches", Namespace: e2eNamespace,
				Immutable: new(true),
				StringData: map[string]string{
					"patches": patches,
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
					// TartBootstrapConfigのmachineConfigurationContextはcluster.Spec.ControlPlaneEndpoint
					// が有効であることを要求する(configuration生成前にAPI serverの到達先を確定させる
					// ため)。labではcontrol-plane VMのIPをDHCP static reservationで固定済みのため、
					// その既知のIPとKubernetes API serverの標準port(6443)を明示設定する。
					ControlPlaneEndpoint: clusterv1.APIEndpoint{
						Host: controlPlaneVMStaticIP,
						Port: 6443,
					},
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
			controllerHealthy := framework.NewControllerPodsHealthyCheck(k8sClient, tartSystemNamespace)
			framework.WaitForConditionUntilTerminal(ctx, tartClusterConditions(e2eNamespace, e2eClusterName), infrav1alpha1.TartClusterReadyCondition, metav1.ConditionTrue, clusterProvisioningTerminalReasons, 20*time.Minute, controllerHealthy)
			framework.WaitForConditionUntilTerminal(ctx, tartControlPlaneConditions(e2eNamespace, e2eClusterName), controlplanev1alpha1.TartControlPlaneAvailableCondition, metav1.ConditionTrue, clusterProvisioningTerminalReasons, 20*time.Minute, controllerHealthy)
			framework.WaitForConditionUntilTerminal(ctx, machineConditionsForCluster(e2eNamespace, e2eClusterName), clusterv1.MachineNodeHealthyCondition, metav1.ConditionTrue, clusterProvisioningTerminalReasons, 20*time.Minute, controllerHealthy)

			var machine clusterv1.Machine
			Eventually(func() error {
				return findMachineForCluster(ctx, e2eNamespace, e2eClusterName, &machine)
			}).WithContext(ctx).WithTimeout(5 * time.Minute).WithPolling(framework.DefaultPollInterval).Should(Succeed())
			waitForTartMachineTalosReady(ctx, machine.Spec.InfrastructureRef.Name, e2eTalosVersion)
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
		if err := k8sClient.List(ctx, &machines, client.InNamespace(namespace)); err != nil {
			return nil, fmt.Errorf("list Machines: %w", err)
		}
		for i := range machines.Items {
			machine := &machines.Items[i]
			if machine.Spec.ClusterName != clusterName {
				continue
			}
			return machine.Status.Conditions, nil
		}
		return nil, fmt.Errorf("no Machine found yet for cluster %s/%s", namespace, clusterName)
	}
}

// systemDiskStableSelectorは、labがsystem diskへ設定したwwidを使ってStableSelectorを解決する。
// virtio-scsi busではlibvirt domain XMLの<disk><serial>がguestまで伝播しないため、Talosの
// hardware discoveryはこれらのdiskのserial fieldを一切報告しない。<wwn>由来のwwidだけが
// package外から観測できる安定識別子である。
func systemDiskStableSelector(host infrav1alpha1.TartHost) string {
	return diskStableSelectorByWWID(host, labDiskWWID("system"))
}

func diskStableSelectorByWWID(host infrav1alpha1.TartHost, wwid string) string {
	if host.Status.Inventory == nil || wwid == "" {
		return ""
	}
	selector := ""
	for _, disk := range host.Status.Inventory.Disks {
		if disk.WWID != wwid {
			continue
		}
		if selector != "" {
			return ""
		}
		selector = disk.StableSelector
	}
	return selector
}

// labDiskWWIDは、Talosが観測するDiskInventory.WWID(およびdisk.wwid CEL式)の表記に合わせ、
// lab.DiskWWNが返す生のhex値へ"naa."prefixを付けて返す。
func labDiskWWID(role string) string {
	return "naa." + lab.DiskWWN(role, controlPlaneVMName)
}

func waitForTartMachineTalosReady(ctx context.Context, name, expectedVersion string) {
	framework.WaitForConditionUntilTerminal(ctx, tartMachineConditions(e2eNamespace, name), infrav1alpha1.TartMachineReadyCondition, metav1.ConditionTrue, clusterProvisioningTerminalReasons, 20*time.Minute)

	var machine infrav1alpha1.TartMachine
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: e2eNamespace, Name: name}, &machine)).To(Succeed())
	for _, conditionType := range []string{
		infrav1alpha1.TartMachineTalosReachableCondition,
		infrav1alpha1.TartMachineProvisionedCondition,
		infrav1alpha1.TartMachineTalosUpToDateCondition,
		infrav1alpha1.TartMachineReadyCondition,
	} {
		condition := meta.FindStatusCondition(machine.Status.Conditions, conditionType)
		Expect(condition).NotTo(BeNil(), "TartMachine condition %q should be reported", conditionType)
		Expect(condition.Status).To(Equal(metav1.ConditionTrue), "TartMachine condition %q should be True", conditionType)
	}
	Expect(machine.Status.TalosVersion).To(Equal(expectedVersion))
	Expect(machine.Status.TalosSchematicID).To(Equal(e2eSchematicID))
}

// labBroadcastAddressは、lab network CIDR上のbroadcast address(host部が全1)にWoL標準port 9を
// 付与したUDP宛先を返す。wol-libvirt-gatewayはlibvirt domainのMACアドレスからVMを解決するため、
// broadcast先はgatewayが listenするlab network上のaddressである必要がある。
func labBroadcastAddress() string {
	// labNetworkCIDRは198.51.100.0/24であるため、broadcastは198.51.100.255固定でよい。
	return "198.51.100.255:9"
}
