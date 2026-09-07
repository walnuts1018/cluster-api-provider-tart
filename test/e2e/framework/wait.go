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
	start := time.Now()
	Eventually(func(g Gomega) {
		conditions, err := get(ctx)
		g.Expect(err).NotTo(HaveOccurred(), "failed to fetch conditions while waiting for %s=%s", conditionType, expectedStatus)
		condition := findCondition(conditions, conditionType)
		g.Expect(condition).NotTo(BeNil(), "condition %s not yet reported", conditionType)
		logConditionHeartbeat(start, conditionType, condition)
		g.Expect(condition.Status).To(Equal(expectedStatus), "condition %s: reason=%s message=%s", conditionType, condition.Reason, condition.Message)
	}).WithContext(ctx).WithTimeout(timeout).WithPolling(DefaultPollInterval).Should(Succeed())
}

// WaitForConditionReasonは、指定したConditionがexpectedStatus兼expectedReasonになるまで待つ。
// fail-closed判定(例: Ready=False/UnsafeUpdate)など、reasonまで含めて観測したい場合に使う。
func WaitForConditionReason(ctx context.Context, get ConditionGetter, conditionType string, expectedStatus metav1.ConditionStatus, expectedReason string, timeout time.Duration) {
	GinkgoHelper()
	start := time.Now()
	Eventually(func(g Gomega) {
		conditions, err := get(ctx)
		g.Expect(err).NotTo(HaveOccurred())
		condition := findCondition(conditions, conditionType)
		g.Expect(condition).NotTo(BeNil(), "condition %s not yet reported", conditionType)
		logConditionHeartbeat(start, conditionType, condition)
		g.Expect(condition.Status).To(Equal(expectedStatus))
		g.Expect(condition.Reason).To(Equal(expectedReason))
	}).WithContext(ctx).WithTimeout(timeout).WithPolling(DefaultPollInterval).Should(Succeed())
}

// terminalReasonDebounceCountは、TerminalReasonsに含まれるreasonを何回連続で観測したら
// fail-fastするかを決める。1回だけの観測で確定させると、正常な遷移の途中で一瞬だけ現れる
// reason(例: 認証済みTalos APIがmaintenance mode到達前にUnavailableを返す等)を恒久障害と
// 誤認しかねないため、必ず複数回の継続観測を要求する。
const terminalReasonDebounceCount = 2

// TerminalReasonsは、reconcilerがtest実行中に外部からの承認や入力なしには自己解決できないと
// 判断できるConditionのReasonの集合である。key=reason文字列、value=人間が読むための説明。
// 一時的に現れるreason(TalosUnreachable等)を含めてはならない — 含めると、正常な起動
// シーケンスの途中でfail-fastが誤発火する。
type TerminalReasons map[string]string

// AbortCheckは、Eventuallyの各pollで追加的に確認する検査である。エラーを返した場合、
// WaitForConditionUntilTerminalはtimeoutを待たずStopTryingで即座に失敗させる。
// TerminalReasonsだけでは検知できない障害(controller-manager PodのCrashLoopBackOff等、
// どのConditionのReasonにも現れずreconcileそのものが止まっているケース)を組み合わせるために使う。
type AbortCheck func(ctx context.Context) error

// WaitForConditionUntilTerminalは、WaitForConditionと同じくpollするが、観測したconditionの
// Reasonがterminal(自己解決不能)であることをterminalReasonDebounceCount回連続で観測した場合、
// または任意のchecksがエラーを返した場合、timeoutを待たずStopTryingで即座に失敗させる。
func WaitForConditionUntilTerminal(ctx context.Context, get ConditionGetter, conditionType string, expectedStatus metav1.ConditionStatus, terminalReasons TerminalReasons, timeout time.Duration, checks ...AbortCheck) {
	GinkgoHelper()
	start := time.Now()
	terminalStreak := 0
	Eventually(func(g Gomega) {
		for _, check := range checks {
			if err := check(ctx); err != nil {
				StopTrying(fmt.Sprintf("aborting wait for condition %s: %s", conditionType, err.Error())).Now()
			}
		}
		conditions, err := get(ctx)
		if err != nil {
			terminalStreak = 0
			g.Expect(err).NotTo(HaveOccurred(), "failed to fetch conditions while waiting for %s=%s", conditionType, expectedStatus)
			return
		}
		condition := findCondition(conditions, conditionType)
		if condition == nil {
			terminalStreak = 0
			g.Expect(condition).NotTo(BeNil(), "condition %s not yet reported", conditionType)
			return
		}
		logConditionHeartbeat(start, conditionType, condition)
		if explanation, terminal := terminalReasons[condition.Reason]; terminal {
			terminalStreak++
			if terminalStreak >= terminalReasonDebounceCount {
				StopTrying(fmt.Sprintf("condition %s reached terminal reason %q (%s): message=%q", conditionType, condition.Reason, explanation, condition.Message)).Now()
			}
		} else {
			terminalStreak = 0
		}
		g.Expect(condition.Status).To(Equal(expectedStatus), "condition %s: reason=%s message=%s", conditionType, condition.Reason, condition.Message)
	}).WithContext(ctx).WithTimeout(timeout).WithPolling(DefaultPollInterval).Should(Succeed())
}

// logConditionHeartbeatは、Eventuallyの各pollで観測したconditionの内容をGinkgoWriterへ出力する。
// 既定では最終timeout失敗時にしかメッセージが出ないため、長い待ちが進行中なのか凍結している
// のかをCI実行中のログからリアルタイムに判別できるようにするための、判定ロジックに影響しない
// 純粋な観測用の出力である。
func logConditionHeartbeat(start time.Time, conditionType string, condition *metav1.Condition) {
	GinkgoWriter.Printf("[%s elapsed] condition %s: status=%s reason=%s message=%q\n",
		time.Since(start).Round(time.Second), conditionType, condition.Status, condition.Reason, condition.Message)
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
