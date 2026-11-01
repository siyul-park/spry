package cli

import (
	"errors"
	"strconv"
)

// actionValue is a boolean flag that asks for a non-run action. CPython acts
// on the first help option it meets, so only the first one set is recorded.
type actionValue struct {
	state  *State
	action Action
}

// choiceValue is a string flag restricted to a set of values.
type choiceValue struct {
	target  *string
	allowed []string
	message string
}

func (v actionValue) Set(string) error {
	if v.state.help == ActionRun {
		v.state.help = v.action
	}
	return nil
}

func (v actionValue) Type() string {
	return "bool"
}

func (v actionValue) String() string {
	return strconv.FormatBool(v.state.help == v.action)
}

func (v choiceValue) Set(value string) error {
	if !contains(v.allowed, value) {
		return errors.New(v.message)
	}
	*v.target = value
	return nil
}

func (v choiceValue) Type() string {
	return "string"
}

func (v choiceValue) String() string {
	return *v.target
}
