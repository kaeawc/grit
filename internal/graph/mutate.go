package graph

import "fmt"

func (g *Graph) SetActionInputs(id ActionID, inputs []ArtifactID) error {
	action, ok := g.actions[id]
	if !ok {
		return fmt.Errorf("action %q does not exist", id)
	}
	for _, input := range inputs {
		if _, ok := g.artifacts[input]; !ok {
			return fmt.Errorf("action %q input artifact %q does not exist", id, input)
		}
	}
	action.Inputs = cloneArtifactIDs(inputs)
	g.actions[id] = cloneAction(action)
	return nil
}
