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
	"k8s.io/apimachinery/pkg/util/wait"
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
			controllerHealthy := framework.NewControllerPodsHealthyCheck(k8sClient, tartSystemNamespace)

			By("waiting for the current InventoryReady condition to be observed at least once")
			framework.WaitForConditionUntilTerminal(ctx, tartHostConditions(e2eHostName), infrav1alpha1.TartHostInventoryReadyCondition, metav1.ConditionTrue, clusterProvisioningTerminalReasons, 15*time.Minute, controllerHealthy)

			By("force-deleting the infrastructure-manager controller Pod")
			Expect(deleteControllerPods(ctx, "infrastructure-controller-manager")).To(Succeed())

			By("confirming the TartHost still converges to InventoryReady=True without manual intervention")
			framework.WaitForConditionUntilTerminal(ctx, tartHostConditions(e2eHostName), infrav1alpha1.TartHostInventoryReadyCondition, metav1.ConditionTrue, clusterProvisioningTerminalReasons, 10*time.Minute, controllerHealthy)
		})

		It("recovers automatically when the bootstrap-manager and control-plane-manager Pods are deleted around configuration apply/reboot", func() {
			By("force-deleting the bootstrap-manager and control-plane-manager controller Pods mid-flight")
			Expect(deleteControllerPods(ctx, "bootstrap-controller-manager")).To(Succeed())
			Expect(deleteControllerPods(ctx, "control-plane-controller-manager")).To(Succeed())

			By("confirming the cluster still converges to Ready without manual intervention")
			controllerHealthy := framework.NewControllerPodsHealthyCheck(k8sClient, tartSystemNamespace)
			framework.WaitForConditionUntilTerminal(ctx, tartClusterConditions(e2eNamespace, e2eClusterName), infrav1alpha1.TartClusterReadyCondition, metav1.ConditionTrue, clusterProvisioningTerminalReasons, 20*time.Minute, controllerHealthy)
		})
	})
}

// deleteControllerPodsは、labelSelector "control-plane=<component>"に一致するcontroller Podを
// kubectl deleteに頼らずclient-go経由で削除する。componentはconfig/manager/*/manager.yamlの
// "control-plane" labelの値("infrastructure-controller-manager"/"bootstrap-controller-manager"/
// "control-plane-controller-manager")と一致させる。
func deleteControllerPods(ctx context.Context, component string) error {
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

	deletedPods := make(map[string]types.UID, len(pods.Items))
	for i := range pods.Items {
		pod := &pods.Items[i]
		deletedPods[pod.Name] = pod.UID
		if err := k8sClient.Delete(ctx, pod, &client.DeleteOptions{GracePeriodSeconds: new(int64(0))}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
	}

	if err := waitDeletedControllerPods(ctx, deletedPods); err != nil {
		return err
	}
	// Podが実際に置き換わり、新しいcontroller-managerがReadyになるまで待つ(Deployment自体は
	// 引き続きReplicas=1のまま自動でPodを再作成する前提)。
	return waitDeploymentPodReady(ctx, component, deletedPods)
}

func waitDeletedControllerPods(ctx context.Context, deletedPods map[string]types.UID) error {
	err := wait.PollUntilContextTimeout(ctx, framework.DefaultPollInterval, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		var pods corev1.PodList
		if err := k8sClient.List(ctx, &pods, client.InNamespace(tartSystemNamespace)); err != nil {
			return false, err
		}
		for i := range pods.Items {
			pod := &pods.Items[i]
			if oldUID, ok := deletedPods[pod.Name]; ok && pod.UID == oldUID {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("wait for deleted controller pods: %w", err)
	}
	return nil
}

func waitDeploymentPodReady(ctx context.Context, component string, deletedPods map[string]types.UID) error {
	start := time.Now()
	Eventually(func(g Gomega) {
		var pods corev1.PodList
		g.Expect(k8sClient.List(ctx, &pods,
			client.InNamespace(tartSystemNamespace),
			client.MatchingLabels{"control-plane": component},
		)).To(Succeed())

		found := false
		ready := false
		for i := range pods.Items {
			pod := &pods.Items[i]
			if oldUID, ok := deletedPods[pod.Name]; ok && pod.UID == oldUID {
				continue
			}
			if pod.DeletionTimestamp != nil {
				continue
			}
			found = true
			GinkgoWriter.Printf("[%s elapsed] pod %s/%s (component %q): phase=%s\n",
				time.Since(start).Round(time.Second), pod.Namespace, pod.Name, component, pod.Status.Phase)
			if pod.Status.Phase != corev1.PodRunning {
				continue
			}
			podReady := false
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodReady {
					podReady = cond.Status == corev1.ConditionTrue
				}
			}
			if podReady {
				ready = true
			}
		}
		g.Expect(found).To(BeTrue(), "no replacement pod found yet for component %q", component)
		g.Expect(ready).To(BeTrue(), "no replacement pod is Ready yet for component %q", component)
	}).WithContext(ctx).WithTimeout(5 * time.Minute).WithPolling(framework.DefaultPollInterval).Should(Succeed())
	return nil
}
