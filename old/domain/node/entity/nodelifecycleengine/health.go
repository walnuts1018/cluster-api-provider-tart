package nodelifecycleengine

import "slices"

// HealthInputはNode Lifecycle Engine Commit前の観測値である。
type HealthInput struct {
	NodeReady     bool
	NodeVersion   string
	TargetVersion string

	StaticPodsReady bool
	EtcdQuorum      bool
	APIHealthy      bool

	NodeRole NodeRole
}

type HealthFailure string

const (
	HealthFailureNodeNotReady       HealthFailure = "NodeNotReady"
	HealthFailureVersionMismatch    HealthFailure = "VersionMismatch"
	HealthFailureStaticPodsNotReady HealthFailure = "StaticPodsNotReady"
	HealthFailureEtcdQuorumLost     HealthFailure = "EtcdQuorumLost"
	HealthFailureAPIUnhealthy       HealthFailure = "APIUnhealthy"
)

// HealthResultはCommit可否と拒否理由を保持する。
type HealthResult struct {
	CommitAllowed bool
	Failures      []HealthFailure
}

func (result HealthResult) HasFailure(failure HealthFailure) bool {
	return slices.Contains(result.Failures, failure)
}

// EvaluateHealthはNode観測値からNode Lifecycle EngineをCommit可能か判定する。
func EvaluateHealth(input HealthInput) HealthResult {
	decision := DecideHealth(input)
	switch result := decision.(type) {
	case HealthGateSatisfied:
		return HealthResult{CommitAllowed: true}
	case HealthGateBlocked:
		failures := make([]HealthFailure, 0, len(result.Failures))
		for _, failure := range result.Failures {
			switch failure.(type) {
			case NodeNotReady:
				failures = append(failures, HealthFailureNodeNotReady)
			case VersionMismatch:
				failures = append(failures, HealthFailureVersionMismatch)
			case StaticPodsNotReady:
				failures = append(failures, HealthFailureStaticPodsNotReady)
			case EtcdQuorumLost:
				failures = append(failures, HealthFailureEtcdQuorumLost)
			case APIUnhealthy:
				failures = append(failures, HealthFailureAPIUnhealthy)
			}
		}
		return HealthResult{
			CommitAllowed: false,
			Failures:      failures,
		}
	default:
		return HealthResult{}
	}
}
