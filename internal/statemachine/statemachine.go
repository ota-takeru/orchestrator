package statemachine

import (
	"fmt"
	"strings"
)

type TransitionSet map[string]map[string]struct{}

type Machine struct {
	name        string
	transitions TransitionSet
	terminal    map[string]struct{}
}

func New(name string, transitions map[string][]string, terminal []string) Machine {
	normalized := make(TransitionSet, len(transitions))
	for from, tos := range transitions {
		normalized[from] = make(map[string]struct{}, len(tos))
		for _, to := range tos {
			normalized[from][to] = struct{}{}
		}
	}
	terminalSet := make(map[string]struct{}, len(terminal))
	for _, status := range terminal {
		terminalSet[status] = struct{}{}
	}
	return Machine{name: name, transitions: normalized, terminal: terminalSet}
}

func (m Machine) Name() string {
	return m.name
}

func (m Machine) CanTransition(from, to string) bool {
	allowed, ok := m.transitions[from]
	if !ok {
		return false
	}
	_, ok = allowed[to]
	return ok
}

func (m Machine) ValidateTransition(from, to string) error {
	if m.CanTransition(from, to) {
		return nil
	}
	if _, ok := m.terminal[from]; ok {
		return fmt.Errorf("%s transition rejected: %s is terminal", m.name, from)
	}
	return fmt.Errorf("%s transition rejected: %s -> %s", m.name, from, to)
}

func (m Machine) IsTerminal(status string) bool {
	_, ok := m.terminal[status]
	return ok
}

func (m Machine) States() []string {
	seen := map[string]struct{}{}
	for from, tos := range m.transitions {
		seen[from] = struct{}{}
		for to := range tos {
			seen[to] = struct{}{}
		}
	}
	for status := range m.terminal {
		seen[status] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for status := range seen {
		out = append(out, status)
	}
	return out
}

func (m Machine) ValidateTimestampShape(status string, startedAtSet, completedAtSet bool) error {
	switch status {
	case "pending":
		if startedAtSet || completedAtSet {
			return fmt.Errorf("%s timestamp rejected: pending must not have started_at or completed_at", m.name)
		}
	case "running":
		if !startedAtSet || completedAtSet {
			return fmt.Errorf("%s timestamp rejected: running must have started_at and no completed_at", m.name)
		}
	default:
		if m.IsTerminal(status) || status == "blocked" {
			if !startedAtSet || !completedAtSet {
				return fmt.Errorf("%s timestamp rejected: %s must have started_at and completed_at", m.name, status)
			}
		}
	}
	return nil
}

func JoinStates(states []string) string {
	return strings.Join(states, ",")
}
