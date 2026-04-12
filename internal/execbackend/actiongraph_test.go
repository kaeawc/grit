package execbackend

import (
	"testing"

	"github.com/kaeawc/grit/internal/graph"
)

// helper to build a minimal graph with actions and artifact-based dependencies.
// Each dep pair (from, to) means "from depends on to" via a shared artifact.
func buildTestGraph(t *testing.T, actionIDs []string, deps [][2]string) (*graph.Graph, []graph.Action) {
	t.Helper()
	g := graph.New()

	if err := g.AddLogicalModule(graph.LogicalModule{
		ID:   "mod",
		Kind: graph.ModuleKindJvmLibrary,
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.AddVariant(graph.Variant{
		ID:       "var",
		ModuleID: "mod",
		Name:     "debug",
	}); err != nil {
		t.Fatal(err)
	}

	// Collect outputs and inputs per action from the dependency edges.
	outputs := map[string][]graph.ArtifactID{}
	inputs := map[string][]graph.ArtifactID{}
	type artSpec struct {
		id       graph.ArtifactID
		producer graph.ActionID
	}
	var artifacts []artSpec

	for _, d := range deps {
		from, to := d[0], d[1]
		artID := graph.ArtifactID("art-" + to + "->" + from)
		artifacts = append(artifacts, artSpec{id: artID, producer: graph.ActionID(to)})
		outputs[to] = append(outputs[to], artID)
		inputs[from] = append(inputs[from], artID)
	}

	// Add artifacts first (with ProducedByActionID set at creation time).
	added := map[graph.ArtifactID]bool{}
	for _, a := range artifacts {
		if added[a.id] {
			continue
		}
		if err := g.AddArtifact(graph.Artifact{
			ID:                 a.id,
			Kind:               graph.ArtifactKindJar,
			ProducedByActionID: a.producer,
		}); err != nil {
			t.Fatal(err)
		}
		added[a.id] = true
	}

	// Add actions.
	actions := make([]graph.Action, 0, len(actionIDs))
	for _, id := range actionIDs {
		a := graph.Action{
			ID:        graph.ActionID(id),
			ModuleID:  "mod",
			VariantID: "var",
			Kind:      graph.ActionKindCompile,
			Outputs:   outputs[id],
			Inputs:    inputs[id],
		}
		if err := g.AddAction(a); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, a)
	}

	return g, actions
}

func TestBuildActionGraph_Empty(t *testing.T) {
	g := graph.New()
	ag, err := BuildActionGraph(g, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ag.Len() != 0 {
		t.Fatalf("expected 0 nodes, got %d", ag.Len())
	}
	if len(ag.TopologicalOrder()) != 0 {
		t.Fatal("expected empty topological order")
	}
}

func TestBuildActionGraph_SingleNode(t *testing.T) {
	g, actions := buildTestGraph(t, []string{"a"}, nil)
	ag, err := BuildActionGraph(g, actions)
	if err != nil {
		t.Fatal(err)
	}
	if ag.Len() != 1 {
		t.Fatalf("expected 1 node, got %d", ag.Len())
	}
	order := ag.TopologicalOrder()
	if len(order) != 1 || order[0] != "a" {
		t.Fatalf("unexpected order: %v", order)
	}
	roots := ag.Roots()
	if len(roots) != 1 || roots[0] != "a" {
		t.Fatalf("unexpected roots: %v", roots)
	}
	leaves := ag.Leaves()
	if len(leaves) != 1 || leaves[0] != "a" {
		t.Fatalf("unexpected leaves: %v", leaves)
	}
}

func TestBuildActionGraph_LinearChain(t *testing.T) {
	// c -> b -> a (c depends on b, b depends on a)
	g, actions := buildTestGraph(t,
		[]string{"a", "b", "c"},
		[][2]string{{"b", "a"}, {"c", "b"}},
	)
	ag, err := BuildActionGraph(g, actions)
	if err != nil {
		t.Fatal(err)
	}
	if ag.Len() != 3 {
		t.Fatalf("expected 3 nodes, got %d", ag.Len())
	}

	order := ag.TopologicalOrder()
	idx := map[graph.ActionID]int{}
	for i, id := range order {
		idx[id] = i
	}
	if idx["a"] >= idx["b"] {
		t.Fatalf("a should come before b in topological order: %v", order)
	}
	if idx["b"] >= idx["c"] {
		t.Fatalf("b should come before c in topological order: %v", order)
	}

	roots := ag.Roots()
	if len(roots) != 1 || roots[0] != "a" {
		t.Fatalf("unexpected roots: %v", roots)
	}
	leaves := ag.Leaves()
	if len(leaves) != 1 || leaves[0] != "c" {
		t.Fatalf("unexpected leaves: %v", leaves)
	}
}

func TestBuildActionGraph_Diamond(t *testing.T) {
	// d depends on b and c; b and c both depend on a.
	g, actions := buildTestGraph(t,
		[]string{"a", "b", "c", "d"},
		[][2]string{{"b", "a"}, {"c", "a"}, {"d", "b"}, {"d", "c"}},
	)
	ag, err := BuildActionGraph(g, actions)
	if err != nil {
		t.Fatal(err)
	}
	if ag.Len() != 4 {
		t.Fatalf("expected 4 nodes, got %d", ag.Len())
	}

	order := ag.TopologicalOrder()
	idx := map[graph.ActionID]int{}
	for i, id := range order {
		idx[id] = i
	}
	if idx["a"] >= idx["b"] || idx["a"] >= idx["c"] {
		t.Fatalf("a should come before b and c: %v", order)
	}
	if idx["b"] >= idx["d"] || idx["c"] >= idx["d"] {
		t.Fatalf("b and c should come before d: %v", order)
	}

	roots := ag.Roots()
	if len(roots) != 1 || roots[0] != "a" {
		t.Fatalf("unexpected roots: %v", roots)
	}
	leaves := ag.Leaves()
	if len(leaves) != 1 || leaves[0] != "d" {
		t.Fatalf("unexpected leaves: %v", leaves)
	}

	nodeD, ok := ag.Node("d")
	if !ok {
		t.Fatal("node d not found")
	}
	if len(nodeD.Dependencies) != 2 {
		t.Fatalf("expected d to have 2 deps, got %d", len(nodeD.Dependencies))
	}
	nodeA, ok := ag.Node("a")
	if !ok {
		t.Fatal("node a not found")
	}
	if len(nodeA.Dependents) != 2 {
		t.Fatalf("expected a to have 2 dependents, got %d", len(nodeA.Dependents))
	}
}

func TestBuildActionGraph_SubsetDropsExternalDeps(t *testing.T) {
	// Full graph: c -> b -> a. But we only select b and c.
	g, allActions := buildTestGraph(t,
		[]string{"a", "b", "c"},
		[][2]string{{"b", "a"}, {"c", "b"}},
	)
	subset := []graph.Action{allActions[1], allActions[2]} // b and c
	ag, err := BuildActionGraph(g, subset)
	if err != nil {
		t.Fatal(err)
	}
	if ag.Len() != 2 {
		t.Fatalf("expected 2 nodes, got %d", ag.Len())
	}

	nodeB, ok := ag.Node("b")
	if !ok {
		t.Fatal("node b not found")
	}
	if len(nodeB.Dependencies) != 0 {
		t.Fatalf("expected b to have 0 deps in subset, got %d: %v", len(nodeB.Dependencies), nodeB.Dependencies)
	}

	roots := ag.Roots()
	if len(roots) != 1 || roots[0] != "b" {
		t.Fatalf("unexpected roots: %v", roots)
	}
}

func TestBuildActionGraph_Nodes(t *testing.T) {
	g, actions := buildTestGraph(t,
		[]string{"a", "b", "c"},
		[][2]string{{"b", "a"}, {"c", "b"}},
	)
	ag, err := BuildActionGraph(g, actions)
	if err != nil {
		t.Fatal(err)
	}

	nodes := ag.Nodes()
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
	if nodes[0].Action.ID != "a" {
		t.Fatalf("expected first node to be a, got %s", nodes[0].Action.ID)
	}
	if nodes[2].Action.ID != "c" {
		t.Fatalf("expected last node to be c, got %s", nodes[2].Action.ID)
	}
}

func TestBuildActionGraph_ParallelRoots(t *testing.T) {
	g, actions := buildTestGraph(t, []string{"x", "y", "z"}, nil)
	ag, err := BuildActionGraph(g, actions)
	if err != nil {
		t.Fatal(err)
	}
	if ag.Len() != 3 {
		t.Fatalf("expected 3 nodes, got %d", ag.Len())
	}
	roots := ag.Roots()
	if len(roots) != 3 {
		t.Fatalf("expected 3 roots, got %d", len(roots))
	}
	leaves := ag.Leaves()
	if len(leaves) != 3 {
		t.Fatalf("expected 3 leaves, got %d", len(leaves))
	}
}
