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

// reconcileRecoverySpecsは、ReconcileRecovery specをginkgoのspec treeへ登録する。suite_test.goの
// 共通Ordered containerからinPlaceUpgradeSpecsの後に呼び出される想定である。実行の前提として
// FreshProvisionでTartHost/e2eHostNameが既にInventoryReady=True(またはそれ以降)まで進んでいる
// 必要がある。ReconcileRecoveryは「controllerが再起動しても手動修復なしで収束すること」
// (program counterではなく外部状態の観測からreconcileを継続できること)を検証する。
func reconcileRecoverySpecs() {
	Describe("ReconcileRecovery", Ordered, func() {
		It("recovers automatically when the infrastructure-manager Pod is deleted after discovery", func() {
			By("waiting for the current InventoryReady condition to be observed at least once")
			framework.WaitForCondition(ctx, tartHostConditions(e2eHostName), infrav1alpha1.TartHostInventoryReadyCondition, metav1.ConditionTrue, 15*time.Minute)

			By("force-deleting the infrastructure-manager controller Pod")
			Expect(deleteControllerPods("infrastructure-controller-manager")).To(Succeed())

			By("confirming the TartHost still converges to InventoryReady=True without manual intervention")
			framework.WaitForCondition(ctx, tartHostConditions(e2eHostName), infrav1alpha1.TartHostInventoryReadyCondition, metav1.ConditionTrue, 10*time.Minute)
		})

		It("recovers automatically when the bootstrap-manager and control-plane-manager Pods are deleted around configuration apply/reboot", func() {
			By("force-deleting the bootstrap-manager and control-plane-manager controller Pods mid-flight")
			Expect(deleteControllerPods("bootstrap-controller-manager")).To(Succeed())
			Expect(deleteControllerPods("control-plane-controller-manager")).To(Succeed())

			By("confirming the cluster still converges to Ready without manual intervention")
			framework.WaitForCondition(ctx, tartClusterConditions(e2eNamespace, e2eClusterName), infrav1alpha1.TartClusterReadyCondition, metav1.ConditionTrue, 20*time.Minute)
		})
	})
}

// deleteControllerPodsは、labelSelector "control-plane=<component>"に一致するcontroller Podを
// kubectl deleteに頼らずclient-go経由で削除する。componentはconfig/manager/*/manager.yamlの
// "control-plane" labelの値("infrastructure-controller-manager"/"bootstrap-controller-manager"/
// "control-plane-controller-manager")と一致させる。
func deleteControllerPods(component string) error {
	var pods corev1.PodList
	if err := k8sClient.List(ctx, &pods,
		client.InNamespace(tartSystemNamespace),
		client.MatchingLabels{"control-plane": component},
	); err != nil {
		return fmt.Errorf("list controller pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no controller pod matched component %q in namespace %q", component, tartSystemNamespace)
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		if err := k8sClient.Delete(ctx, pod); err != nil {
			return fmt.Errorf("delete pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
	}

	// Podが実際に置き換わり、新しいcontroller-managerがReadyになるまで待つ(Deployment自体は
	// 引き続きReplicas=1のまま自動でPodを再作成する前提)。
	return waitDeploymentPodReady(component)
}

func waitDeploymentPodReady(component string) error {
	Eventually(func(g Gomega) {
		var pods corev1.PodList
		g.Expect(k8sClient.List(context.Background(), &pods,
			client.InNamespace(tartSystemNamespace),
			client.MatchingLabels{"control-plane": component},
		)).To(Succeed())

		found := false
		for i := range pods.Items {
			pod := &pods.Items[i]
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
