//go:build e2e

package framework

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ControllerManagerComponentsは、config/manager/*/manager.yamlが設定する"control-plane"
// labelの値の一覧である。3つのTart provider controller-manager Deploymentを横断して
// pod healthを確認する際の対象を表す。
var ControllerManagerComponents = []string{
	"infrastructure-controller-manager",
	"bootstrap-controller-manager",
	"control-plane-controller-manager",
}

// NewControllerPodsHealthyCheckは、指定namespace内のcontroller-manager Podのいずれかが
// CrashLoopBackOffに陥っていないかを確認するAbortCheckを返す。ReconcileがどのCondition
// Reasonにも現れない形で停止するケース(panicループ等)を捕捉するための安全網であり、
// WaitForConditionUntilTerminalのTerminalReasons判定とは別軸のチェックである。
func NewControllerPodsHealthyCheck(c client.Client, namespace string) AbortCheck {
	return func(ctx context.Context) error {
		for _, component := range ControllerManagerComponents {
			var pods corev1.PodList
			if err := c.List(ctx, &pods, client.InNamespace(namespace), client.MatchingLabels{"control-plane": component}); err != nil {
				return fmt.Errorf("list controller pods for component %q: %w", component, err)
			}
			for i := range pods.Items {
				pod := &pods.Items[i]
				for _, containerStatus := range pod.Status.ContainerStatuses {
					if containerStatus.State.Waiting != nil && containerStatus.State.Waiting.Reason == "CrashLoopBackOff" {
						return fmt.Errorf("controller pod %s/%s (component %q) is CrashLoopBackOff: %s",
							pod.Namespace, pod.Name, component, containerStatus.State.Waiting.Message)
					}
				}
			}
		}
		return nil
	}
}
