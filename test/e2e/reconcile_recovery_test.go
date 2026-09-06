//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/test/e2e/framework"
)

const tartSystemNamespace = "cluster-api-provider-tart-system"

// Ordered継続実行の前提: FreshProvisionでTartHost/e2eHostNameが既にInventoryReady=True(またはそれ以降)
// まで進んでいる必要がある。ReconcileRecoveryは「controllerが再起動しても手動修復なしで収束すること」
// (program counterではなく外部状態の観測からreconcileを継続できること)を検証する。
var _ = Describe("ReconcileRecovery", Ordered, func() {
	It("recovers automatically when the infrastructure-manager Pod is deleted after discovery", func() {
		By("waiting for the current InventoryReady condition to be observed at least once")
		framework.WaitForCondition(ctx, tartHostConditions(e2eHostName), infrav1alpha1.TartHostInventoryReadyCondition, metav1.ConditionTrue, 15*time.Minute)

		By("force-deleting the infrastructure-manager controller Pod")
		Expect(deleteControllerPods("infrastructure-manager")).To(Succeed())

		By("confirming the TartHost still converges to InventoryReady=True without manual intervention")
		framework.WaitForCondition(ctx, tartHostConditions(e2eHostName), infrav1alpha1.TartHostInventoryReadyCondition, metav1.ConditionTrue, 10*time.Minute)
	})

	It("recovers automatically when the bootstrap-manager and control-plane-manager Pods are deleted around configuration apply/reboot", func() {
		By("force-deleting the bootstrap-manager and control-plane-manager controller Pods mid-flight")
		Expect(deleteControllerPods("bootstrap-manager")).To(Succeed())
		Expect(deleteControllerPods("control-plane-manager")).To(Succeed())

		By("confirming the cluster still converges to Ready without manual intervention")
		framework.WaitForCondition(ctx, tartClusterConditions(e2eNamespace, e2eClusterName), infrav1alpha1.TartClusterReadyCondition, metav1.ConditionTrue, 20*time.Minute)
	})
})

// deleteControllerPodsは、labelSelector "control-plane=controller-manager,app=<component>"相当の
// controller Podをkubectl deleteに頼らずclient-go経由で削除する。componentは
// "infrastructure-manager"/"bootstrap-manager"/"control-plane-manager"のいずれかを渡す。
//
// TODO: 実際のDeployment labelがcomponent別にどう分かれているかはconfig/manager配下の
// manager.yamlの実際のlabel設定に依存する。骨格実装ではcontrol-plane=controller-managerに
// 加えてDeployment名からPodを絞り込む簡易実装とし、実CI実行時にlabelが一致しない場合は
// config/manager/*/manager.yamlのlabelに合わせて絞り込み条件を調整する。
func deleteControllerPods(component string) error {
	var pods corev1.PodList
	if err := k8sClient.List(ctx, &pods,
		client.InNamespace(tartSystemNamespace),
		client.MatchingLabels{"control-plane": "controller-manager"},
	); err != nil {
		return fmt.Errorf("list controller pods: %w", err)
	}

	deleted := 0
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !podBelongsToComponent(pod, component) {
			continue
		}
		if err := k8sClient.Delete(ctx, pod); err != nil {
			return fmt.Errorf("delete pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
		deleted++
	}
	if deleted == 0 {
		return fmt.Errorf("no controller pod matched component %q in namespace %q", component, tartSystemNamespace)
	}

	// Podが実際に置き換わり、新しいcontroller-managerがReadyになるまで待つ(Deployment自体は
	// 引き続きReplicas=1のまま自動でPodを再作成する前提)。
	return waitDeploymentPodReady(component)
}

func podBelongsToComponent(pod *corev1.Pod, component string) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name == component {
			return true
		}
	}
	// Deployment名がPod名のprefixになっている前提のfallback。
	return len(pod.Name) > len(component) && pod.Name[:len(component)] == component
}

func waitDeploymentPodReady(component string) error {
	Eventually(func(g Gomega) {
		var pods corev1.PodList
		g.Expect(k8sClient.List(context.Background(), &pods,
			client.InNamespace(tartSystemNamespace),
			client.MatchingLabels{"control-plane": "controller-manager"},
		)).To(Succeed())

		found := false
		for i := range pods.Items {
			pod := &pods.Items[i]
			if !podBelongsToComponent(pod, component) {
				continue
			}
			found = true
			g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning))
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodReady {
					g.Expect(cond.Status).To(Equal(corev1.ConditionTrue))
				}
			}
		}
		g.Expect(found).To(BeTrue(), "no replacement pod found yet for component %q", component)
	}).WithTimeout(5 * time.Minute).WithPolling(framework.DefaultPollInterval).Should(Succeed())
	return nil
}
