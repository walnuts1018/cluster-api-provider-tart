package nodelifecycleengine

type PlanResult interface {
	isPlanResult()
}

type PlanReady struct {
	Plan Plan
}

type PlanRejected struct {
	Failure Failure
}

type RunnableDecision interface {
	isRunnableDecision()
}

type StepRunnable struct{}

type StepBlocked struct {
	Failure Failure
}

type HealthDecision interface {
	isHealthDecision()
}

type HealthGateSatisfied struct{}

type HealthGateBlocked struct {
	Failures []HealthGateFailure
}

func (PlanReady) isPlanResult()          {}
func (PlanRejected) isPlanResult()       {}
func (StepRunnable) isRunnableDecision() {}
func (StepBlocked) isRunnableDecision()  {}

func (HealthGateSatisfied) isHealthDecision() {}
func (HealthGateBlocked) isHealthDecision()   {}
