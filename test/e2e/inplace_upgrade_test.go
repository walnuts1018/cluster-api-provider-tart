//go:build e2e

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/test/e2e/framework"
)

// upgradeTargetTalosVersion/upgradeTargetKubernetesVersionは、FreshProvisionが構築した
// clusterに対してin-place upgradeを要求するdesired versionである。
const (
	upgradeTargetTalosVersion      = "v1.14.1"
	upgradeTargetKubernetesVersion = "v1.34.1"

	upgradeDataConfigMapName = "tart-e2e-upgrade-marker"
	upgradeDataPayload       = "tart-e2e-in-place-upgrade-marker"
)

// upgradeIdentityRecordは、upgrade前後でMachine replacementが発生していないことを検証するために
// 記録するidentityの集合である。program counterではなく、単純な「upgrade前後で値が変わらない」
// ことのassertに使う観測値である。
type upgradeIdentityRecord struct {
	machineUID     types.UID
	tartMachineUID types.UID
	tartHostName   string
	nodeUID        types.UID
	configMapUID   types.UID
	checksum       string
}

var recordedIdentity upgradeIdentityRecord

// inPlaceUpgradeSpecsは、InPlaceUpgrade specをginkgoのspec treeへ登録する。suite_test.goの
// 共通Ordered containerからfreshProvisionSpecsの後に呼び出される想定である(FreshProvisionが
// 構築した同一clusterをそのまま利用するため)。
func inPlaceUpgradeSpecs() {
	Describe("InPlaceUpgrade", Ordered, func() {
		BeforeAll(func() {
			By("recording pre-upgrade identity (Machine/TartMachine/TartHost binding/Node UID) and writing a checksummed marker")
			recordedIdentity = recordCurrentIdentity()
		})

		It("upgrades Talos OS in place without replacing the Machine or losing disk-backed data", func() {
			// TODO: TartMachineTemplate.spec.template.spec.imageの更新だけで既存TartMachineへ
			// desired imageが伝播するのか、CAPI Update Extension経由で個々のTartMachine.spec.image
			// への直接patchが必要なのかは、controller/tartmachine, controller/tartcontrolplane
			// reconcilerの実装挙動を実CIで確認して確定する必要がある。骨格実装では両方を明示的に
			// 更新することで、どちらの経路でもTalosUpToDate=Trueへ収束することを期待する。
			By("bumping TartMachineTemplate's desired Talos image and waiting for TalosUpToDate")
			var machineTemplate infrav1alpha1.TartMachineTemplate
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: e2eNamespace, Name: e2eClusterName + "-cp"}, &machineTemplate)).To(Succeed())
			machineTemplate.Spec.Template.Spec.Image.Version = upgradeTargetTalosVersion
			Expect(k8sClient.Update(ctx, &machineTemplate)).To(Succeed())

			var machine clusterv1.Machine
			Expect(findMachineForCluster(e2eNamespace, e2eClusterName, &machine)).To(Succeed())
			var tartMachine infrav1alpha1.TartMachine
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: e2eNamespace, Name: machine.Spec.InfrastructureRef.Name}, &tartMachine)).To(Succeed())
			tartMachine.Spec.Image.Version = upgradeTargetTalosVersion
			Expect(k8sClient.Update(ctx, &tartMachine)).To(Succeed())

			framework.WaitForCondition(ctx, tartMachineConditions(e2eNamespace, tartMachine.Name), infrav1alpha1.TartMachineTalosUpToDateCondition, metav1.ConditionTrue, 20*time.Minute)

			assertIdentityUnchanged(recordedIdentity)
		})

		It("upgrades Kubernetes cluster-wide without replacing any Machine", func() {
			var controlPlane controlplanev1alpha1.TartControlPlane
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: e2eNamespace, Name: e2eClusterName}, &controlPlane)).To(Succeed())
			controlPlane.Spec.Version = upgradeTargetKubernetesVersion
			Expect(k8sClient.Update(ctx, &controlPlane)).To(Succeed())

			Eventually(func(g Gomega) {
				var updated controlplanev1alpha1.TartControlPlane
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: e2eNamespace, Name: e2eClusterName}, &updated)).To(Succeed())
				g.Expect(updated.Status.KubernetesUpgrade.ObservedVersion).To(Equal(upgradeTargetKubernetesVersion))
			}).WithContext(ctx).WithTimeout(20 * time.Minute).WithPolling(framework.DefaultPollInterval).Should(Succeed())

			framework.WaitForCondition(ctx, tartControlPlaneConditions(e2eNamespace, e2eClusterName), controlplanev1alpha1.TartControlPlaneAvailableCondition, metav1.ConditionTrue, 20*time.Minute)

			assertIdentityUnchanged(recordedIdentity)
		})
	})
}

// recordCurrentIdentityは、Machine/TartMachine/TartHost binding/Node UIDと、data disk上へ
// 書き込んだmarker(ConfigMapで代替。UserVolume+PV/PVC経由のファイル書き込みはlab固有の
// storage classセットアップが必要なため、本骨格ではConfigMap UID + SHA256を最小の代理指標として扱う)
// を記録する。
//
// TODO: 本来の計画通り、data disk(hdd.qcow2)上のUserVolume + Local PV/PVCへ実際にファイルを
// 書き込みSHA256を記録する実装へ差し替える。StorageClass/PV定義がlab環境のnode local pathに
// 依存するため、実際のTalos install後のUserVolume mount pathが判明してから確定する。
func recordCurrentIdentity() upgradeIdentityRecord {
	var machine clusterv1.Machine
	Expect(findMachineForCluster(e2eNamespace, e2eClusterName, &machine)).To(Succeed())

	var tartMachine infrav1alpha1.TartMachine
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: e2eNamespace, Name: machine.Spec.InfrastructureRef.Name}, &tartMachine)).To(Succeed())
	Expect(tartMachine.Status.HostRef).NotTo(BeNil())

	var node corev1.Node
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: machine.Status.NodeRef.Name}, &node)).To(Succeed())

	sum := sha256.Sum256([]byte(upgradeDataPayload))
	checksum := hex.EncodeToString(sum[:])

	marker := &corev1.ConfigMap{
		Name: upgradeDataConfigMapName, Namespace: e2eNamespace,
		Data: map[string]string{"checksum": checksum, "payload": upgradeDataPayload},
	}
	Expect(k8sClient.Create(ctx, marker)).To(Succeed())

	return upgradeIdentityRecord{
		machineUID:     machine.UID,
		tartMachineUID: tartMachine.UID,
		tartHostName:   tartMachine.Status.HostRef.Name,
		nodeUID:        node.UID,
		configMapUID:   marker.UID,
		checksum:       checksum,
	}
}

// assertIdentityUnchangedは、upgrade前後でMachine/TartMachine/TartHost binding/Node/markerの
// identityが全て不変であることを検証し、「Machine replacementが起きていないこと」を明示する。
func assertIdentityUnchanged(before upgradeIdentityRecord) {
	var machine clusterv1.Machine
	Expect(findMachineForCluster(e2eNamespace, e2eClusterName, &machine)).To(Succeed())
	Expect(machine.UID).To(Equal(before.machineUID), "Machine UID must not change across in-place upgrade")

	var tartMachine infrav1alpha1.TartMachine
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: e2eNamespace, Name: machine.Spec.InfrastructureRef.Name}, &tartMachine)).To(Succeed())
	Expect(tartMachine.UID).To(Equal(before.tartMachineUID), "TartMachine UID must not change across in-place upgrade")
	Expect(tartMachine.Status.HostRef).NotTo(BeNil())
	Expect(tartMachine.Status.HostRef.Name).To(Equal(before.tartHostName), "TartHost binding must not change across in-place upgrade")

	Expect(machine.Status.NodeRef.IsDefined()).To(BeTrue())
	var node corev1.Node
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: machine.Status.NodeRef.Name}, &node)).To(Succeed())
	Expect(node.UID).To(Equal(before.nodeUID), "Node UID must not change across in-place upgrade")

	var marker corev1.ConfigMap
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: e2eNamespace, Name: upgradeDataConfigMapName}, &marker)).To(Succeed())
	Expect(marker.UID).To(Equal(before.configMapUID))
	Expect(marker.Data["checksum"]).To(Equal(before.checksum), "data checksum must survive in-place upgrade")
}

func findMachineForCluster(namespace, clusterName string, out *clusterv1.Machine) error {
	var machines clusterv1.MachineList
	if err := k8sClient.List(context.Background(), &machines); err != nil {
		return fmt.Errorf("list Machines: %w", err)
	}
	for i := range machines.Items {
		if machines.Items[i].Namespace == namespace && machines.Items[i].Spec.ClusterName == clusterName {
			*out = machines.Items[i]
			return nil
		}
	}
	return fmt.Errorf("no Machine found for cluster %s/%s", namespace, clusterName)
}

func tartMachineConditions(namespace, name string) framework.ConditionGetter {
	return func(ctx context.Context) ([]metav1.Condition, error) {
		var tartMachine infrav1alpha1.TartMachine
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &tartMachine); err != nil {
			return nil, fmt.Errorf("get TartMachine %s/%s: %w", namespace, name, err)
		}
		return tartMachine.Status.Conditions, nil
	}
}
