package host

// EligibilityはHostをallocation対象にできるかという観測結果である。workflow phaseとして保存せず、spec.consumerRef、spec.retainedFrom、spec.reusePolicy、spec.reuseApproval、spec.reuseModeから常に再計算する。
type Eligibility string

const (
	// Available Hostにはconsumer参照と保持記録がないため、決定論的なHost allocatorがclaimできる。
	Available Eligibility = "Available"
	// Claimed Hostは特定のTartMachineへbindされている。
	Claimed Eligibility = "Claimed"
	// Retained Hostは以前のMachineのdataまたはTalos identityを保持するため、現在の保持記録に一致する明示的なreuse approvalがあるまで自動allocationしない。
	Retained Eligibility = "Retained"
	// Reusable Hostは一致するreuse approvalとreuse modeを持つRetained Hostである。AdoptまたはReprovisionの対象にはできるが、reuseModeを無視した通常のclaim経路では選択しない。
	Reusable Eligibility = "Reusable"
)
