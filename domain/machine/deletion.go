package machine

import "slices"

// DrainCompleteは、CAPI Machineの削除がinfra側のHost解放を進めてよい段階まで進んでいるかを判定する。
// 呼び出し側(usecase層)は、CAPI MachineがDeletionTimestamp付きであること、MachineDeletingCondition
// がTrueであること、およびその時点のreasonをこの純粋関数へ渡す。completionReasonsには、
// drain/volume detach完了を示すCAPI core側のreason文字列一覧を渡す(clusterv1のreason定数への
// 依存はusecase層に閉じ込め、domainはCAPI core SDK型を一切importしない)。
func DrainComplete(hasDeletionTimestamp, deletingConditionTrue bool, reason string, completionReasons []string) bool {
	if !hasDeletionTimestamp || !deletingConditionTrue {
		return false
	}
	return slices.Contains(completionReasons, reason)
}
