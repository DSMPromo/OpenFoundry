// Package compiler lowers a Pipeline Builder graph
// (libs/pipeline-expression.Graph) to an executable Spark SQL plan.
//
// The compiler runs in three phases:
//
//   1. Topologically sort the nodes so every Inputs reference is
//      defined before it is read.
//   2. Emit `CREATE OR REPLACE TEMP VIEW node_<short_hash_id> AS …`
//      for every intermediate node, translating the kind-specific
//      config into Spark SQL.
//   3. Emit one terminal statement per output node — `INSERT INTO` /
//      `INSERT OVERWRITE` / `MERGE INTO` depending on write_mode.
//
// The result is a `CompiledPlan` that mirrors the shape
// `pipeline-runner-spark` already accepts: statements separated by
// `;`, last statement is the writer, inputs / outputs lifted into the
// envelope so the runner can pre-resolve dataset RIDs without
// re-parsing SQL.
//
// This file ships the framework + per-row kinds (filter, select,
// derived_column, cast). Joins, aggregations, windows, and reshape
// land in B2.2.
package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	pe "github.com/openfoundry/openfoundry-go/libs/pipeline-expression"
)

// CompiledPlan is the envelope the Spark runner consumes.
// Statements are executed in order; the last statement is always the
// terminal write for the LAST output node. Multiple outputs emit
// multiple writer statements at the tail in topological order.
type CompiledPlan struct {
	// Statements is the ordered SQL stream.
	Statements []string `json:"statements"`

	// Inputs lifted from every dataset_input node so the runner can
	// pre-resolve RIDs without re-parsing SQL.
	Inputs []DatasetRef `json:"inputs"`

	// Outputs lifted from every dataset_output node, in topological
	// order — the runner uses this to know which transactions to
	// open / commit.
	Outputs []OutputBinding `json:"outputs"`

	// EstimatedShuffle is a heuristic byte count of how much data
	// will move across the cluster. Populated by B2.2 once join /
	// aggregate cardinality estimates are in place; for B2.1 it's 0.
	EstimatedShuffle int64 `json:"estimated_shuffle"`
}

// DatasetRef points at a dataset input — the runner resolves the RID
// + branch to a concrete view name (`spark_catalog.<schema>.<table>`)
// before substituting the placeholder.
type DatasetRef struct {
	NodeID     string `json:"node_id"`
	ViewName   string `json:"view_name"`
	DatasetRID string `json:"dataset_rid"`
	Branch     string `json:"branch,omitempty"`
}

// OutputBinding identifies one terminal write — the runner opens a
// transaction per binding before executing Statements.
type OutputBinding struct {
	NodeID     string `json:"node_id"`
	DatasetRID string `json:"dataset_rid"`
	Branch     string `json:"branch,omitempty"`
	WriteMode  string `json:"write_mode"`
}

// Error is one compilation failure. Multiple errors are returned as a
// MultiError so the UI can surface every problem at once.
type Error struct {
	NodeID  string
	Message string
}

func (e *Error) Error() string {
	if e.NodeID == "" {
		return e.Message
	}
	return fmt.Sprintf("node %s: %s", e.NodeID, e.Message)
}

// MultiError aggregates per-node compilation errors.
type MultiError struct {
	Errors []*Error
}

func (m *MultiError) Error() string {
	if m == nil || len(m.Errors) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m.Errors))
	for _, e := range m.Errors {
		parts = append(parts, e.Error())
	}
	return "compile failed: " + strings.Join(parts, "; ")
}

// Compile lowers the graph to a CompiledPlan or returns a MultiError.
// Structural validation (Validate(g)) is run first; if that surfaces
// any errors the compile stops immediately.
func Compile(g pe.Graph) (*CompiledPlan, error) {
	if errs := pe.Validate(g); len(errs) > 0 {
		me := &MultiError{}
		for _, e := range errs {
			me.Errors = append(me.Errors, &Error{NodeID: e.NodeID, Message: e.Message})
		}
		return nil, me
	}

	ordered, err := topologicalOrder(g)
	if err != nil {
		return nil, err
	}

	plan := &CompiledPlan{}
	for _, node := range ordered {
		switch {
		case node.Kind.IsInput():
			ref, refErr := compileInput(node)
			if refErr != nil {
				return nil, refErr
			}
			plan.Inputs = append(plan.Inputs, ref)
		case node.Kind.IsOutput():
			// Outputs land last — collect for the terminal pass.
			continue
		default:
			stmt, intermediateErr := compileIntermediate(node)
			if intermediateErr != nil {
				return nil, intermediateErr
			}
			plan.Statements = append(plan.Statements, stmt)
		}
	}

	// Terminal writers — emit in topological order so dependent
	// outputs (one output reading another via a checkpoint) come last.
	for _, node := range ordered {
		if !node.Kind.IsOutput() {
			continue
		}
		stmt, binding, outErr := compileOutput(node)
		if outErr != nil {
			return nil, outErr
		}
		plan.Statements = append(plan.Statements, stmt)
		plan.Outputs = append(plan.Outputs, binding)
	}

	return plan, nil
}

// topologicalOrder is a deterministic Kahn's-algorithm walk. Sorts
// the available-roots set by ID at every step so the same graph
// produces the same statement order across runs (golden-test
// friendly).
func topologicalOrder(g pe.Graph) ([]pe.Node, error) {
	byID := make(map[string]pe.Node, len(g.Nodes))
	for _, node := range g.Nodes {
		byID[node.ID] = node
	}
	inDegree := make(map[string]int, len(g.Nodes))
	dependents := make(map[string][]string, len(g.Nodes))
	for _, node := range g.Nodes {
		inDegree[node.ID] += 0 // ensure entry exists
		for _, input := range node.Inputs {
			inDegree[node.ID]++
			dependents[input] = append(dependents[input], node.ID)
		}
	}

	var ready []string
	for id, deg := range inDegree {
		if deg == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)

	ordered := make([]pe.Node, 0, len(g.Nodes))
	for len(ready) > 0 {
		next := ready[0]
		ready = ready[1:]
		ordered = append(ordered, byID[next])
		for _, child := range dependents[next] {
			inDegree[child]--
			if inDegree[child] == 0 {
				ready = append(ready, child)
			}
		}
		sort.Strings(ready)
	}
	if len(ordered) != len(g.Nodes) {
		return nil, &Error{Message: "topological sort failed — cycle present (Validate should have caught this)"}
	}
	return ordered, nil
}

// viewName derives the stable temp-view name for a node ID. The same
// node always lowers to the same view name so re-compiling a graph
// for a partial re-run can reuse cached views.
func viewName(nodeID string) string {
	sum := sha256.Sum256([]byte(nodeID))
	return "node_" + hex.EncodeToString(sum[:6])
}

// upstreamViewName looks up the view name a node should read from.
// Equivalent to viewName(node.Inputs[0]) but exists as a named helper
// so the per-kind compilers read intent rather than indexing.
func upstreamViewName(node pe.Node) (string, error) {
	if len(node.Inputs) == 0 {
		return "", &Error{NodeID: node.ID, Message: "transform node missing input"}
	}
	return viewName(node.Inputs[0]), nil
}

// compileInput lifts a dataset_input into a DatasetRef. The runner
// substitutes the ViewName placeholder with the resolved Spark table
// before executing — no SQL is emitted by this compiler for inputs.
func compileInput(node pe.Node) (DatasetRef, error) {
	var cfg pe.DatasetInputConfig
	if err := node.UnmarshalConfig(&cfg); err != nil {
		return DatasetRef{}, &Error{NodeID: node.ID, Message: err.Error()}
	}
	if strings.TrimSpace(cfg.DatasetRID) == "" {
		return DatasetRef{}, &Error{NodeID: node.ID, Message: "dataset_rid is required"}
	}
	return DatasetRef{
		NodeID:     node.ID,
		ViewName:   viewName(node.ID),
		DatasetRID: cfg.DatasetRID,
		Branch:     cfg.Branch,
	}, nil
}

// compileIntermediate lowers a single non-input / non-output node to
// a CREATE OR REPLACE TEMP VIEW statement.
func compileIntermediate(node pe.Node) (string, error) {
	upstream, err := upstreamViewName(node)
	if err != nil {
		return "", err
	}
	var body string
	switch node.Kind {
	case pe.NodeFilter:
		body, err = compileFilter(node, upstream)
	case pe.NodeSelect:
		body, err = compileSelect(node, upstream)
	case pe.NodeDerivedColumn:
		body, err = compileDerivedColumn(node, upstream)
	case pe.NodeCast:
		body, err = compileCast(node, upstream)
	case pe.NodeCheckpoint:
		// Checkpoint is a pass-through at compile time — the runner
		// physically materializes the staging dataset, but the SQL
		// reads it as if it were any other intermediate.
		body = "SELECT * FROM " + upstream
	default:
		return "", &Error{
			NodeID:  node.ID,
			Message: fmt.Sprintf("node kind %q is not yet supported by the compiler (planned for B2.2)", node.Kind),
		}
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("CREATE OR REPLACE TEMP VIEW %s AS\n%s", viewName(node.ID), body), nil
}

func compileFilter(node pe.Node, upstream string) (string, error) {
	var cfg pe.FilterConfig
	if err := node.UnmarshalConfig(&cfg); err != nil {
		return "", &Error{NodeID: node.ID, Message: err.Error()}
	}
	if strings.TrimSpace(cfg.Predicate) == "" {
		return "", &Error{NodeID: node.ID, Message: "filter predicate is required"}
	}
	return fmt.Sprintf("SELECT *\nFROM %s\nWHERE %s", upstream, cfg.Predicate), nil
}

func compileSelect(node pe.Node, upstream string) (string, error) {
	var cfg pe.SelectConfig
	if err := node.UnmarshalConfig(&cfg); err != nil {
		return "", &Error{NodeID: node.ID, Message: err.Error()}
	}
	if len(cfg.Columns) == 0 {
		return "", &Error{NodeID: node.ID, Message: "select requires at least one column"}
	}
	quoted := make([]string, len(cfg.Columns))
	for i, col := range cfg.Columns {
		quoted[i] = quoteIdent(col)
	}
	return fmt.Sprintf("SELECT %s\nFROM %s", strings.Join(quoted, ", "), upstream), nil
}

func compileDerivedColumn(node pe.Node, upstream string) (string, error) {
	var cfg pe.DerivedColumnConfig
	if err := node.UnmarshalConfig(&cfg); err != nil {
		return "", &Error{NodeID: node.ID, Message: err.Error()}
	}
	if len(cfg.Columns) == 0 {
		return "", &Error{NodeID: node.ID, Message: "derived_column requires at least one column"}
	}
	projections := make([]string, len(cfg.Columns)+1)
	projections[0] = "*"
	for i, col := range cfg.Columns {
		if strings.TrimSpace(col.Name) == "" || strings.TrimSpace(col.Expression) == "" {
			return "", &Error{NodeID: node.ID, Message: "derived_column entries need name + expression"}
		}
		projections[i+1] = fmt.Sprintf("(%s) AS %s", col.Expression, quoteIdent(col.Name))
	}
	return fmt.Sprintf("SELECT %s\nFROM %s", strings.Join(projections, ", "), upstream), nil
}

func compileCast(node pe.Node, upstream string) (string, error) {
	var cfg pe.CastConfig
	if err := node.UnmarshalConfig(&cfg); err != nil {
		return "", &Error{NodeID: node.ID, Message: err.Error()}
	}
	if len(cfg.Casts) == 0 {
		return "", &Error{NodeID: node.ID, Message: "cast requires at least one column"}
	}
	// Spark doesn't let us overwrite columns in one projection without
	// listing every other column. The compiler renames the cast
	// projection to a sentinel, drops the original, then renames back.
	// For B2.1 we keep it simple: emit `SELECT CAST(col AS type) AS col, …`
	// and let users include passthrough columns in derived_column if
	// they want them. This matches Foundry's surface.
	projections := make([]string, len(cfg.Casts))
	for i, c := range cfg.Casts {
		if strings.TrimSpace(c.Column) == "" || strings.TrimSpace(c.TargetType) == "" {
			return "", &Error{NodeID: node.ID, Message: "cast entries need column + target_type"}
		}
		projections[i] = fmt.Sprintf("CAST(%s AS %s) AS %s", quoteIdent(c.Column), c.TargetType, quoteIdent(c.Column))
	}
	return fmt.Sprintf("SELECT %s\nFROM %s", strings.Join(projections, ", "), upstream), nil
}

// compileOutput emits the terminal writer for a dataset_output node.
// SNAPSHOT → INSERT OVERWRITE; APPEND → INSERT INTO; UPDATE → MERGE
// (placeholder for B2.2 — currently errors). DELETE is reserved.
func compileOutput(node pe.Node) (string, OutputBinding, error) {
	var cfg pe.DatasetOutputConfig
	if err := node.UnmarshalConfig(&cfg); err != nil {
		return "", OutputBinding{}, &Error{NodeID: node.ID, Message: err.Error()}
	}
	if strings.TrimSpace(cfg.DatasetRID) == "" {
		return "", OutputBinding{}, &Error{NodeID: node.ID, Message: "dataset_rid is required"}
	}
	upstream, err := upstreamViewName(node)
	if err != nil {
		return "", OutputBinding{}, err
	}
	mode := strings.ToUpper(strings.TrimSpace(cfg.WriteMode))
	if mode == "" {
		mode = "SNAPSHOT"
	}
	var stmt string
	switch mode {
	case "SNAPSHOT":
		stmt = fmt.Sprintf("INSERT OVERWRITE %s\nSELECT * FROM %s", outputPlaceholder(node.ID), upstream)
	case "APPEND":
		stmt = fmt.Sprintf("INSERT INTO %s\nSELECT * FROM %s", outputPlaceholder(node.ID), upstream)
	case "UPDATE", "DELETE":
		return "", OutputBinding{}, &Error{
			NodeID:  node.ID,
			Message: fmt.Sprintf("write_mode %s is not yet supported (planned for B2.2)", mode),
		}
	default:
		return "", OutputBinding{}, &Error{NodeID: node.ID, Message: "unknown write_mode " + mode}
	}
	return stmt, OutputBinding{
		NodeID:     node.ID,
		DatasetRID: cfg.DatasetRID,
		Branch:     cfg.Branch,
		WriteMode:  mode,
	}, nil
}

// outputPlaceholder returns the placeholder the runner substitutes
// with the resolved Iceberg table FQN. Same shape as the inputs.
func outputPlaceholder(nodeID string) string {
	return "{{output:" + nodeID + "}}"
}

// quoteIdent backtick-quotes an identifier for Spark SQL. Backticks
// inside the identifier are doubled — matches Spark's escape rule.
func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// Render returns the joined SQL stream as one `;`-separated script.
// Convenience for callers that want a single string to ship to the
// Spark driver.
func (p *CompiledPlan) Render() string {
	return strings.Join(p.Statements, ";\n\n") + ";\n"
}
