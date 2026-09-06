package update

import bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"

// Decideは分類結果とapply strategyから最終的なDecisionを決定する。純粋関数でありTalos machineryへの依存を持たない。
func Decide(class ChangeClass, reason string, strategy bootstrapv1alpha1.ConfigurationApplyStrategy) (Decision, error) {
	switch class {
	case ChangeNone:
		return Decision{Class: ChangeNone}, nil
	case ChangeInvariantConflict, ChangeReprovisionRequired:
		return Decision{Class: class, Reason: reason}, nil
	case ChangeUpdatable:
		// strategyに従って適用modeを決める。
	default:
		return Decision{}, nil
	}
	mode, err := ResolveApplyMode(strategy)
	if err != nil {
		return Decision{}, err
	}
	return Decision{Class: ChangeUpdatable, ApplyMode: mode, Reason: reason}, nil
}
