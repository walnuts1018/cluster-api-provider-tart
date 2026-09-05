package resourcefinalizer

import "fmt"

type DesiredState interface {
	isDesiredState()
}

type ObservedState interface {
	isObservedState()
}

type Command interface {
	isCommand()
}

type DesiredPresent struct{}
type DesiredAbsent struct{}

type ObservedPresent struct{}
type ObservedAbsent struct{}

type CommandAdd struct{}
type CommandRemove struct{}
type CommandNoop struct{}

func (DesiredPresent) isDesiredState() {}
func (DesiredAbsent) isDesiredState()  {}

func (ObservedPresent) isObservedState() {}
func (ObservedAbsent) isObservedState()  {}

func (CommandAdd) isCommand()    {}
func (CommandRemove) isCommand() {}
func (CommandNoop) isCommand()   {}

func Decide(desired DesiredState, observed ObservedState) (Command, error) {
	switch desired.(type) {
	case DesiredPresent:
		switch observed.(type) {
		case ObservedPresent:
			return CommandNoop{}, nil
		case ObservedAbsent:
			return CommandAdd{}, nil
		default:
			return nil, fmt.Errorf("unknown resource finalizer observed state: %T", observed)
		}
	case DesiredAbsent:
		switch observed.(type) {
		case ObservedPresent:
			return CommandRemove{}, nil
		case ObservedAbsent:
			return CommandNoop{}, nil
		default:
			return nil, fmt.Errorf("unknown resource finalizer observed state: %T", observed)
		}
	default:
		return nil, fmt.Errorf("unknown resource finalizer desired state: %T", desired)
	}
}
