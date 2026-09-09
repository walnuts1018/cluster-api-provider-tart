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
	"k8s.io/apimachinery/pkg/util/wait"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
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

		It("retains a Host after Machine deletion and does not automatically reclaim it", func() {
			var machine clusterv1.Machine
			Expect(findMachineForCluster(ctx, e2eNamespace, e2eClusterName, &machine)).To(Succeed())

			// TartHost.Spec.ConsumerRefはCAPIのcore Machineではなく、controller/tartmachineが
			// bindingの単位として扱うinfrastructure Machine(TartMachine)のUIDを保持する
			// (controller/tartmachine/reconciler.goのconsumer構築を参照)。そのためHost claim/
			// shutdown confirmationの照合には、core MachineではなくTartMachineのUIDを使う。
			var tartMachine infrav1alpha1.TartMachine
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: e2eNamespace, Name: machine.Spec.InfrastructureRef.Name}, &tartMachine)).To(Succeed())

			var host infrav1alpha1.TartHost
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: e2eHostName}, &host)).To(Succeed())
			Expect(host.Spec.ConsumerRef).NotTo(BeNil())
			Expect(host.Spec.ConsumerRef.UID).To(Equal(tartMachine.UID))

			// WoLではcontrollerが停止を観測できないため、Machine削除後のHost claim解除には
			// 現在のTartMachine、HostID、BootIDを結び付けた明示的confirmationが必要になる。
			confirmation := &infrav1alpha1.ShutdownConfirmation{
				ConsumerUID: tartMachine.UID,
				HostID:      host.Spec.HostID,
			}
			if host.Status.Inventory != nil {
				confirmation.BootID = host.Status.Inventory.BootID
			}

			By("deleting the CAPI Machine and confirming the current Host shutdown target")
			Expect(k8sClient.Delete(ctx, &machine)).To(Succeed())
			Expect(updateOnConflict(ctx, func() error {
				var current infrav1alpha1.TartHost
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: e2eHostName}, &current); err != nil {
					return err
				}
				current.Spec.ShutdownConfirmation = confirmation
				return k8sClient.Update(ctx, &current)
			})).To(Succeed())

			By("waiting for the Host to become retained without an active consumer")
			Eventually(func(g Gomega) {
				var current infrav1alpha1.TartHost
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: e2eHostName}, &current)).To(Succeed())
				g.Expect(current.Spec.ConsumerRef).To(BeNil())
				g.Expect(current.Spec.PreviousConsumerRef).NotTo(BeNil())
				g.Expect(current.Spec.PreviousConsumerRef.UID).To(Equal(tartMachine.UID))
				available := meta.FindStatusCondition(current.Status.Conditions, infrav1alpha1.TartHostAvailableCondition)
				g.Expect(available).NotTo(BeNil())
				g.Expect(available.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(available.Reason).To(Equal(infrav1alpha1.ReasonRetained))
			}).WithContext(ctx).WithTimeout(15 * time.Minute).WithPolling(framework.DefaultPollInterval).Should(Succeed())
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
