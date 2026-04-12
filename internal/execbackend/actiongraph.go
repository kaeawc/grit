package execbackend

import (
	"fmt"
	"sort"

	"github.com/kaeawc/grit/internal/graph"
)

// ActionNode is a frozen snapshot of a graph.Action along with its direct
// dependency and dependent edges within the action graph.
type ActionNode struct {
	Action       graph.Action
	Dependencies []graph.ActionID
	Dependents   []graph.ActionID
}

// ActionGraph is an immutable DAG of concrete actions with dependency edges,
// derived from the higher-level build graph. Once constructed the graph cannot
// be modified, which enables upfront validation, cache probing, and scheduling
// optimisation before execution begins.
type ActionGraph struct {
	nodes map[graph.ActionID]ActionNode
	order []graph.ActionID // topologically sorted; populated on construction
}

// BuildActionGraph constructs an immutable ActionGraph from the given actions
// and build graph. It resolves dependency edges via artifact input/output
// relationships (using g.ActionDependencies), validates that the result is a
// DAG, and computes a topological ordering.
//
// Only actions in the provided slice are included; dependency edges that point
// outside the selected set are silently dropped.
func BuildActionGraph(g *graph.Graph, actions []graph.Action) (*ActionGraph, error) {
	if len(actions) == 0 {
		return &ActionGraph{
			nodes: map[graph.ActionID]ActionNode{},
			order: nil,
		}, nil
	}

	// Index selected actions.
	selected := make(map[graph.ActionID]graph.Action, len(actions))
	for _, a := range actions {
		selected[a.ID] = a
	}

	// Build adjacency lists (only within the selected set).
	deps := make(map[graph.ActionID][]graph.ActionID, len(actions))
	dependents := make(map[graph.ActionID][]graph.ActionID, len(actions))
	indegree := make(map[graph.ActionID]int, len(actions))

	for _, a := range actions {
		indegree[a.ID] = 0
	}
	for _, a := range actions {
		upstream := g.ActionDependencies(a.ID)
		for _, dep := range upstream {
			if _, ok := selected[dep]; !ok {
				continue
			}
			deps[a.ID] = append(deps[a.ID], dep)
			dependents[dep] = append(dependents[dep], a.ID)
			indegree[a.ID]++
		}
		// Sort dependency lists for determinism.
		sort.Slice(deps[a.ID], func(i, j int) bool { return deps[a.ID][i] < deps[a.ID][j] })
	}
	for id := range dependents {
		sort.Slice(dependents[id], func(i, j int) bool { return dependents[id][i] < dependents[id][j] })
	}

	// Kahn's algorithm for topological sort (also detects cycles).
	ready := make([]graph.ActionID, 0, len(actions))
	for id, deg := range indegree {
		if deg == 0 {
			ready = append(ready, id)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i] < ready[j] })

	order := make([]graph.ActionID, 0, len(actions))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		order = append(order, id)
		for _, dep := range dependents[id] {
			indegree[dep]--
			if indegree[dep] == 0 {
				ready = append(ready, dep)
				sort.Slice(ready, func(i, j int) bool { return ready[i] < ready[j] })
			}
		}
	}

	if len(order) != len(actions) {
		return nil, fmt.Errorf("action graph contains a cycle (%d of %d actions ordered)", len(order), len(actions))
	}

	// Freeze nodes.
	nodes := make(map[graph.ActionID]ActionNode, len(actions))
	for _, a := range actions {
		nodes[a.ID] = ActionNode{
			Action:       a,
			Dependencies: cloneActionIDs(deps[a.ID]),
			Dependents:   cloneActionIDs(dependents[a.ID]),
		}
	}

	return &ActionGraph{
		nodes: nodes,
		order: order,
	}, nil
}

// Len returns the number of action nodes in the graph.
func (ag *ActionGraph) Len() int {
	return len(ag.nodes)
}

// Node returns the ActionNode for the given ID. The second return value is
// false if the ID is not present.
func (ag *ActionGraph) Node(id graph.ActionID) (ActionNode, bool) {
	n, ok := ag.nodes[id]
	return n, ok
}

// Nodes returns all ActionNodes in topological order.
func (ag *ActionGraph) Nodes() []ActionNode {
	out := make([]ActionNode, 0, len(ag.order))
	for _, id := range ag.order {
		out = append(out, ag.nodes[id])
	}
	return out
}

// TopologicalOrder returns the action IDs in topological order (dependencies
// before dependents). The order is deterministic for a given input.
func (ag *ActionGraph) TopologicalOrder() []graph.ActionID {
	out := make([]graph.ActionID, len(ag.order))
	copy(out, ag.order)
	return out
}

// Roots returns the action IDs that have no dependencies within this graph.
func (ag *ActionGraph) Roots() []graph.ActionID {
	out := make([]graph.ActionID, 0)
	for _, id := range ag.order {
		if len(ag.nodes[id].Dependencies) == 0 {
			out = append(out, id)
		}
	}
	return out
}

// Leaves returns the action IDs that have no dependents within this graph.
func (ag *ActionGraph) Leaves() []graph.ActionID {
	out := make([]graph.ActionID, 0)
	for _, id := range ag.order {
		if len(ag.nodes[id].Dependents) == 0 {
			out = append(out, id)
		}
	}
	return out
}

func cloneActionIDs(ids []graph.ActionID) []graph.ActionID {
	if len(ids) == 0 {
		return nil
	}
	out := make([]graph.ActionID, len(ids))
	copy(out, ids)
	return out
}
