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

// allowed defines valid transitions: from -> []to
var allowed = map[State][]State{
	StateIdle:       {StateReasoning},
	StateReasoning:  {StateActing, StateReflecting, StateResponding},
	StateActing:     {StateReasoning, StateResponding},
	StateReflecting: {StateReasoning, StateResponding},
	StateResponding: {StateIdle},
}

type fsm struct {
	current State
}

func (f *fsm) transition(to State) error {
	for _, s := range allowed[f.current] {
		if s == to {
			f.current = to
			return nil
		}
	}
	return fmt.Errf("invalid FSM transition: %s -> %s", f.current, to)
}
