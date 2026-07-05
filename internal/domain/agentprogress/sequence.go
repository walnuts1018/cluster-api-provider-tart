package agentprogress

type Decision string

const (
	DecisionApply     Decision = "Apply"
	DecisionDuplicate Decision = "Duplicate"
	DecisionGap       Decision = "Gap"
	DecisionInvalid   Decision = "Invalid"
)

func EvaluateSequence(saved, incoming int64) Decision {
	switch {
	case saved < 0 || incoming <= 0:
		return DecisionInvalid
	case incoming <= saved:
		return DecisionDuplicate
	case incoming == saved+1:
		return DecisionApply
	case incoming > saved+1:
		return DecisionGap
	}
	return DecisionInvalid
}
