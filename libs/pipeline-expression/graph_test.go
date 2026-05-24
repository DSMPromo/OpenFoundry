package pipelineexpression

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGraphRoundTripsThroughJSON(t *testing.T) {
	g := Graph{
		Version: "1",
		Nodes: []Node{
			{
				ID:     "src",
				Kind:   NodeDatasetInput,
				Config: mustMarshalT(t,DatasetInputConfig{DatasetRID: "ri.dataset.x", Branch: "master"}),
			},
			{
				ID:     "flt",
				Kind:   NodeFilter,
				Inputs: []string{"src"},
				Config: mustMarshalT(t,FilterConfig{Predicate: "amount > 0"}),
			},
			{
				ID:     "out",
				Kind:   NodeDatasetOutput,
				Inputs: []string{"flt"},
				Config: mustMarshalT(t,DatasetOutputConfig{DatasetRID: "ri.dataset.y", WriteMode: "SNAPSHOT"}),
			},
		},
	}
	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	var back Graph
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Version != "1" || len(back.Nodes) != 3 {
		t.Fatalf("round-trip mismatch: %+v", back)
	}

	// Per-kind config helpers.
	var src DatasetInputConfig
	if err := back.Nodes[0].UnmarshalConfig(&src); err != nil {
		t.Fatal(err)
	}
	if src.DatasetRID != "ri.dataset.x" {
		t.Errorf("input cfg = %+v", src)
	}
}

func TestValidateCatchesUnknownKind(t *testing.T) {
	g := Graph{
		Version: "1",
		Nodes: []Node{
			{ID: "x", Kind: "bogus"},
		},
	}
	errs := Validate(g)
	if !containsMessage(errs, `unknown kind "bogus"`) {
		t.Errorf("expected unknown-kind error; got %+v", errs)
	}
}

func TestValidateCatchesUnknownInputReference(t *testing.T) {
	g := Graph{
		Version: "1",
		Nodes: []Node{
			{ID: "a", Kind: NodeDatasetInput},
			{ID: "b", Kind: NodeFilter, Inputs: []string{"missing"}},
		},
	}
	errs := Validate(g)
	if !containsMessage(errs, `unknown input reference "missing"`) {
		t.Errorf("expected unknown-input error; got %+v", errs)
	}
}

func TestValidateCatchesDuplicateID(t *testing.T) {
	g := Graph{
		Version: "1",
		Nodes: []Node{
			{ID: "dup", Kind: NodeDatasetInput},
			{ID: "dup", Kind: NodeFilter, Inputs: []string{"dup"}},
		},
	}
	errs := Validate(g)
	if !containsMessage(errs, "duplicate id") {
		t.Errorf("expected duplicate-id error; got %+v", errs)
	}
}

func TestValidateCatchesCycle(t *testing.T) {
	g := Graph{
		Version: "1",
		Nodes: []Node{
			{ID: "a", Kind: NodeFilter, Inputs: []string{"c"}},
			{ID: "b", Kind: NodeFilter, Inputs: []string{"a"}},
			{ID: "c", Kind: NodeFilter, Inputs: []string{"b"}},
		},
	}
	errs := Validate(g)
	if !containsMessage(errs, "cycle detected") {
		t.Errorf("expected cycle error; got %+v", errs)
	}
}

func TestValidateRejectsEmptyGraph(t *testing.T) {
	errs := Validate(Graph{Version: "1"})
	if !containsMessage(errs, "no nodes") {
		t.Errorf("expected no-nodes error; got %+v", errs)
	}
}

func TestValidateRejectsInputsOnInputNode(t *testing.T) {
	g := Graph{
		Version: "1",
		Nodes: []Node{
			{ID: "x", Kind: NodeDatasetInput, Inputs: []string{"y"}},
			{ID: "y", Kind: NodeDatasetInput},
		},
	}
	errs := Validate(g)
	if !containsMessage(errs, "input nodes cannot have Inputs") {
		t.Errorf("expected input-with-inputs error; got %+v", errs)
	}
}

func TestValidateRejectsTransformWithoutInputs(t *testing.T) {
	g := Graph{
		Version: "1",
		Nodes: []Node{
			{ID: "lonely", Kind: NodeFilter},
		},
	}
	errs := Validate(g)
	if !containsMessage(errs, "transform nodes require at least one input") {
		t.Errorf("expected transform-no-input error; got %+v", errs)
	}
}

func TestValidateAcceptsHappyPath(t *testing.T) {
	g := Graph{
		Version: "1",
		Nodes: []Node{
			{ID: "src", Kind: NodeDatasetInput, Config: mustMarshalT(t,DatasetInputConfig{DatasetRID: "ri.dataset.x"})},
			{ID: "agg", Kind: NodeAggregate, Inputs: []string{"src"}, Config: mustMarshalT(t,AggregateConfig{
				GroupBy:      []string{"region"},
				Aggregations: []AggregateProjection{{Name: "total", Function: "SUM", Expression: "amount"}},
			})},
			{ID: "out", Kind: NodeDatasetOutput, Inputs: []string{"agg"}, Config: mustMarshalT(t,DatasetOutputConfig{DatasetRID: "ri.dataset.y"})},
		},
	}
	errs := Validate(g)
	if len(errs) != 0 {
		t.Errorf("happy path should validate clean; got %+v", errs)
	}
}

func TestEveryKindIsKnown(t *testing.T) {
	for _, kind := range AllNodeKinds {
		if !kind.IsKnown() {
			t.Errorf("%s missing from IsKnown()", kind)
		}
	}
	if NodeKind("nope").IsKnown() {
		t.Error("unknown kind should not be known")
	}
}

func TestJoinKindEnumeration(t *testing.T) {
	for _, j := range AllJoinKinds {
		if !j.IsKnown() {
			t.Errorf("%s missing from JoinKind.IsKnown()", j)
		}
	}
	if JoinKind("bogus").IsKnown() {
		t.Error("unknown join kind should not be known")
	}
}

// containsMessage returns true when any error's Message contains needle.
func containsMessage(errs []ValidationError, needle string) bool {
	for _, e := range errs {
		if strings.Contains(e.Message, needle) {
			return true
		}
	}
	return false
}

func mustMarshalT(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
