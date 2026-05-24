package compiler

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pe "github.com/openfoundry/openfoundry-go/libs/pipeline-expression"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files from current compiler output")

// readGraph parses a test fixture from testdata/<name>.graph.json.
func readGraph(t *testing.T, name string) pe.Graph {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g pe.Graph
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// assertGolden compares actual to testdata/<name>.golden.sql. With
// -update, the file is rewritten so the maintainer can review the
// diff on the next commit.
func assertGolden(t *testing.T, name, actual string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden.sql")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(actual), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := actual; got != string(want) {
		t.Errorf("golden mismatch for %s\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}

func TestCompileMinimalDatasetPassthrough(t *testing.T) {
	g := readGraph(t, "minimal_passthrough")
	plan, err := Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Inputs) != 1 || plan.Inputs[0].DatasetRID != "ri.dataset.x" {
		t.Errorf("inputs = %+v", plan.Inputs)
	}
	if len(plan.Outputs) != 1 || plan.Outputs[0].WriteMode != "SNAPSHOT" {
		t.Errorf("outputs = %+v", plan.Outputs)
	}
	assertGolden(t, "minimal_passthrough", plan.Render())
}

func TestCompileFilterSelectDerived(t *testing.T) {
	g := readGraph(t, "filter_select_derived")
	plan, err := Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "filter_select_derived", plan.Render())
}

func TestCompileAppendWriteMode(t *testing.T) {
	g := readGraph(t, "append_mode")
	plan, err := Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Outputs[0].WriteMode != "APPEND" {
		t.Errorf("write_mode = %q", plan.Outputs[0].WriteMode)
	}
	if !strings.Contains(plan.Render(), "INSERT INTO") {
		t.Errorf("expected INSERT INTO; got:\n%s", plan.Render())
	}
	assertGolden(t, "append_mode", plan.Render())
}

func TestCompileRejectsUnsupportedKind(t *testing.T) {
	g := pe.Graph{
		Version: "1",
		Nodes: []pe.Node{
			{ID: "src", Kind: pe.NodeDatasetInput, Config: mustMarshal(t, pe.DatasetInputConfig{DatasetRID: "ri.x"})},
			{ID: "agg", Kind: pe.NodeAggregate, Inputs: []string{"src"}, Config: mustMarshal(t, pe.AggregateConfig{GroupBy: []string{"a"}})},
			{ID: "out", Kind: pe.NodeDatasetOutput, Inputs: []string{"agg"}, Config: mustMarshal(t, pe.DatasetOutputConfig{DatasetRID: "ri.y"})},
		},
	}
	_, err := Compile(g)
	if err == nil {
		t.Fatal("expected error for unsupported kind")
	}
	if !strings.Contains(err.Error(), "B2.2") {
		t.Errorf("unsupported-kind error should mention B2.2, got: %v", err)
	}
}

func TestCompileRejectsCyclicGraph(t *testing.T) {
	g := pe.Graph{
		Version: "1",
		Nodes: []pe.Node{
			{ID: "a", Kind: pe.NodeFilter, Inputs: []string{"b"}, Config: mustMarshal(t, pe.FilterConfig{Predicate: "x > 0"})},
			{ID: "b", Kind: pe.NodeFilter, Inputs: []string{"a"}, Config: mustMarshal(t, pe.FilterConfig{Predicate: "y > 0"})},
		},
	}
	_, err := Compile(g)
	if err == nil {
		t.Fatal("expected error for cycle")
	}
}

func TestCompileTopologicalOrderIsDeterministic(t *testing.T) {
	g := readGraph(t, "filter_select_derived")
	got1, err := Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	if got1.Render() != got2.Render() {
		t.Errorf("non-deterministic compile output:\n%s\nvs\n%s", got1.Render(), got2.Render())
	}
}

func TestCompileRejectsUnknownWriteMode(t *testing.T) {
	g := pe.Graph{
		Version: "1",
		Nodes: []pe.Node{
			{ID: "src", Kind: pe.NodeDatasetInput, Config: mustMarshal(t, pe.DatasetInputConfig{DatasetRID: "ri.x"})},
			{ID: "out", Kind: pe.NodeDatasetOutput, Inputs: []string{"src"}, Config: mustMarshal(t, pe.DatasetOutputConfig{DatasetRID: "ri.y", WriteMode: "BOGUS"})},
		},
	}
	_, err := Compile(g)
	if err == nil {
		t.Fatal("expected error for unknown write_mode")
	}
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
