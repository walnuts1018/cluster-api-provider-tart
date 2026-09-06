package machine

import "time"

// ShutdownRequestedReasonは、Talos shutdownを要求済みだがまだAPI到達不能を確認できていない間、
// TartMachineのReady Conditionへ設定するreasonである。usecase/controller層はこの定数を用いて
// Ready Conditionのreasonを設定・判定し、文字列を直接埋め込まない。
const ShutdownRequestedReason = "ShutdownRequested"

// IsShutdownRequestedは、観測されたReady Conditionのreasonからshutdown要求済みかを判定する。
func IsShutdownRequested(readyConditionReason string) bool {
	return readyConditionReason == ShutdownRequestedReason
}

// IsShutdownSettledは、shutdown要求後の確認待ち時間が経過しているかを判定する。
// transitionedAtがゼロ値(未設定)の場合は経過とみなさない。
func IsShutdownSettled(readyConditionReason string, transitionedAt, now time.Time, delay time.Duration) bool {
	if !IsShutdownRequested(readyConditionReason) {
		return false
	}
	if transitionedAt.IsZero() {
		return false
	}
	return now.Sub(transitionedAt) >= delay
}
