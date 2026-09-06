//go:build e2e

package framework

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:staticcheck
	. "github.com/onsi/gomega"    //nolint:staticcheck

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DefaultPollIntervalはEventuallyの既定poll間隔である。
	DefaultPollInterval = 5 * time.Second
)

// ConditionGetterは、対象Resourceが持つmetav1.Condition一覧を返す。TartHost/TartMachine/
// TartCluster/TartControlPlane等、Statusにconditionsを持つ全Resourceで共通利用できるよう
// interfaceにしていない代わりに、呼び出し側がclient.Objectを取得した後にfetch関数を渡す形にする。
type ConditionGetter func(ctx context.Context) ([]metav1.Condition, error)

// WaitForConditionは、指定したConditionがexpectedStatusになるまでEventuallyでpollする。
// shell sleepではなくAPI observationだけに依存し、program counter的な状態を一切参照しない。
func WaitForCondition(ctx context.Context, get ConditionGetter, conditionType string, expectedStatus metav1.ConditionStatus, timeout time.Duration) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		conditions, err := get(ctx)
		g.Expect(err).NotTo(HaveOccurred(), "failed to fetch conditions while waiting for %s=%s", conditionType, expectedStatus)
		condition := findCondition(conditions, conditionType)
		g.Expect(condition).NotTo(BeNil(), "condition %s not yet reported", conditionType)
		g.Expect(condition.Status).To(Equal(expectedStatus), "condition %s: reason=%s message=%s", conditionType, condition.Reason, condition.Message)
	}).WithContext(ctx).WithTimeout(timeout).WithPolling(DefaultPollInterval).Should(Succeed())
}

// WaitForConditionReasonは、指定したConditionがexpectedStatus兼expectedReasonになるまで待つ。
// fail-closed判定(例: Ready=False/UnsafeUpdate)など、reasonまで含めて観測したい場合に使う。
func WaitForConditionReason(ctx context.Context, get ConditionGetter, conditionType string, expectedStatus metav1.ConditionStatus, expectedReason string, timeout time.Duration) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		conditions, err := get(ctx)
		g.Expect(err).NotTo(HaveOccurred())
		condition := findCondition(conditions, conditionType)
		g.Expect(condition).NotTo(BeNil(), "condition %s not yet reported", conditionType)
		g.Expect(condition.Status).To(Equal(expectedStatus))
		g.Expect(condition.Reason).To(Equal(expectedReason))
	}).WithContext(ctx).WithTimeout(timeout).WithPolling(DefaultPollInterval).Should(Succeed())
}

func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

// WaitForObjectはgetがerrorなしで完了するまで(=objectが存在するようになるまで)待つ。
func WaitForObject(ctx context.Context, c client.Client, key client.ObjectKey, obj client.Object, timeout time.Duration) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		g.Expect(c.Get(ctx, key, obj)).To(Succeed())
	}).WithContext(ctx).WithTimeout(timeout).WithPolling(DefaultPollInterval).Should(Succeed())
}

// WaitForDeletionはobjectが削除完了(NotFound)になるまで待つ。
func WaitForDeletion(ctx context.Context, c client.Client, key client.ObjectKey, obj client.Object, timeout time.Duration) {
	GinkgoHelper()
	Eventually(func(g Gomega) error {
		err := c.Get(ctx, key, obj)
		if err == nil {
			return fmt.Errorf("object %s still exists", key)
		}
		return client.IgnoreNotFound(err)
	}).WithContext(ctx).WithTimeout(timeout).WithPolling(DefaultPollInterval).Should(Succeed())
}
