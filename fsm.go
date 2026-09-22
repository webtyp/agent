package agent

import "webtyp.com/fmt"

type State uint8

const (
	StateIdle State = iota
	StateReasoning
	StateActing
	StateReflecting
	StateResponding
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "Idle"
	case StateReasoning:
		return "Reasoning"
	case StateActing:
		return "Acting"
	case StateReflecting:
		return "Reflecting"
	case StateResponding:
		return "Responding"
	default:
		return fmt.Sprintf("Unknown(%d)", s)
	}
}

func allowedFrom(s State) []State {
	switch s {
	case StateIdle:
		return []State{StateReasoning}
	case StateReasoning:
		return []State{StateActing, StateReflecting, StateResponding}
	case StateActing:
		return []State{StateReasoning, StateResponding}
	case StateReflecting:
		return []State{StateReasoning, StateResponding}
	case StateResponding:
		return []State{StateIdle}
	default:
		return nil
	}
}

type fsm struct {
	current State
}

func (f *fsm) transition(to State) error {
	for _, s := range allowedFrom(f.current) {
		if s == to {
			f.current = to
			return nil
		}
	}
	return fmt.Errf("invalid FSM transition: %s -> %s", f.current, to)
}
