package tasks

import (
	"errors"
	"fmt"
)

type Node struct {
	ID           string
	Dependencies []string
}

func ValidateDAG(nodes []Node) error {
	byID := map[string]Node{}
	for _, n := range nodes {
		if n.ID == "" {
			return errors.New("task ID is required")
		}
		if _, ok := byID[n.ID]; ok {
			return fmt.Errorf("duplicate task %s", n.ID)
		}
		byID[n.ID] = n
	}
	state := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return fmt.Errorf("task dependency cycle contains %s", id)
		}
		if state[id] == 2 {
			return nil
		}
		node, ok := byID[id]
		if !ok {
			return fmt.Errorf("missing task dependency %s", id)
		}
		state[id] = 1
		for _, dep := range node.Dependencies {
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}
	for id := range byID {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
