package pipelineexpression

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Graph is the canonical JSON contract for a Pipeline Builder logic
// graph. The schema is the source of truth that
//   - pipeline-build-service persists in `pipeline_authoring.draft_dag`
//     / `published_dag`,
//   - apps/web saves from the visual graph editor, and
//   - the upcoming B2 compiler in libs/pipeline-expression lowers to a
//     Spark SQL plan.
//
// Parity targets (TASKS_COMPUTE_PIPELINES.md Task B1):
// https://www.palantir.com/docs/foundry/pipeline-builder/logic
//
// JSON field names mirror the Foundry persisted shape so the existing
// per-node `transform_type` validator (node_check.go) can be migrated
// to this schema incrementally without breaking persisted graphs.
type Graph struct {
	// Version is the schema version. Currently "1"; bump only on
	// breaking changes (renaming or removing existing kinds, changing
	// required config keys). New kinds are additive.
	Version string `json:"version"`

	Nodes []Node `json:"nodes"`

	// Edges are an *optional* projection of Node.Inputs. When present,
	// edges win — the UI renders from edges so per-port wiring can be
	// preserved across rounds. When absent, Inputs is the source of
	// truth.
	Edges []Edge `json:"edges,omitempty"`
}

// Node is one operation in the graph. Config is JSON-typed so each
// kind carries its own shape; helpers below unmarshal the kind-
// appropriate struct.
type Node struct {
	ID     string   `json:"id"`
	Kind   NodeKind `json:"kind"`
	Inputs []string `json:"inputs,omitempty"`

	// Config carries kind-specific settings (e.g. FilterConfig,
	// JoinConfig). Use Node.UnmarshalConfig(target) to decode.
	Config json.RawMessage `json:"config,omitempty"`

	// Position is a UI hint preserved for the visual editor; the
	// compiler ignores it.
	Position *NodePosition `json:"position,omitempty"`

	// DisplayName is the user-facing label. Compiler ignores.
	DisplayName string `json:"display_name,omitempty"`
}

// Edge represents an explicit port connection between two nodes. The
// FromPort / ToPort fields exist for nodes that have more than one
// output / input port (e.g. join's `left` / `right`).
type Edge struct {
	From     string `json:"from"`
	FromPort string `json:"from_port,omitempty"`
	To       string `json:"to"`
	ToPort   string `json:"to_port,omitempty"`
}

// NodePosition is the visual editor's x/y placement. Persisted so the
// graph round-trips through save/load.
type NodePosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// NodeKind enumerates every Pipeline Builder node type that the
// compiler / validator know how to handle. New kinds are additive;
// removing one is a breaking schema change.
type NodeKind string

const (
	// Inputs / outputs.
	NodeDatasetInput         NodeKind = "dataset_input"
	NodeDatasetOutput        NodeKind = "dataset_output"
	NodeMediaSetInput        NodeKind = "media_set_input"
	NodeMediaSetOutput       NodeKind = "media_set_output"
	NodeVirtualTableOutput   NodeKind = "virtual_table_output"
	NodeOntologyObjectOutput NodeKind = "ontology_object_output"

	// Per-row.
	NodeFilter         NodeKind = "filter"
	NodeSelect         NodeKind = "select"
	NodeDerivedColumn  NodeKind = "derived_column"
	NodeCast           NodeKind = "cast"
	NodeMediaTransform NodeKind = "media_transform"

	// Multi-input set ops.
	NodeJoin      NodeKind = "join"
	NodeUnion     NodeKind = "union"
	NodeIntersect NodeKind = "intersect"
	NodeExcept    NodeKind = "except"
	NodeGeoJoin   NodeKind = "geo_join"

	// Aggregations / window functions.
	NodeAggregate           NodeKind = "aggregate"
	NodeAggregateOverWindow NodeKind = "aggregate_over_window"
	NodeProjectOverWindow   NodeKind = "project_over_window"

	// Reshape.
	NodePivot   NodeKind = "pivot"
	NodeUnpivot NodeKind = "unpivot"

	// Order / rank.
	NodeSort NodeKind = "sort"
	NodeRank NodeKind = "rank"

	// Reliability.
	NodeCheckpoint NodeKind = "checkpoint"
)

// AllNodeKinds lists every NodeKind. Useful for exhaustiveness tests
// and for the OpenAPI enum generator.
var AllNodeKinds = []NodeKind{
	NodeDatasetInput, NodeDatasetOutput, NodeMediaSetInput, NodeMediaSetOutput,
	NodeVirtualTableOutput, NodeOntologyObjectOutput,
	NodeFilter, NodeSelect, NodeDerivedColumn, NodeCast, NodeMediaTransform,
	NodeJoin, NodeUnion, NodeIntersect, NodeExcept, NodeGeoJoin,
	NodeAggregate, NodeAggregateOverWindow, NodeProjectOverWindow,
	NodePivot, NodeUnpivot,
	NodeSort, NodeRank,
	NodeCheckpoint,
}

// IsKnown reports whether the kind is in AllNodeKinds.
func (k NodeKind) IsKnown() bool {
	for _, candidate := range AllNodeKinds {
		if k == candidate {
			return true
		}
	}
	return false
}

// IsOutput reports whether the kind is a terminal output node — the
// DAG walker uses this to find roots of the reverse-dependency graph.
func (k NodeKind) IsOutput() bool {
	switch k {
	case NodeDatasetOutput, NodeMediaSetOutput, NodeVirtualTableOutput, NodeOntologyObjectOutput:
		return true
	}
	return false
}

// IsInput reports whether the kind is a leaf input node — sources
// have no Inputs.
func (k NodeKind) IsInput() bool {
	switch k {
	case NodeDatasetInput, NodeMediaSetInput:
		return true
	}
	return false
}

// JoinKind enumerates the supported join semantics. Mirrors Spark
// SQL's `Join` types plus Pipeline Builder's `knn` and `lookup`
// extensions.
type JoinKind string

const (
	JoinInner JoinKind = "inner"
	JoinLeft  JoinKind = "left"
	JoinRight JoinKind = "right"
	JoinOuter JoinKind = "outer"
	JoinAnti  JoinKind = "anti"
	JoinSemi  JoinKind = "semi"
	JoinCross JoinKind = "cross"
	JoinKNN   JoinKind = "knn"
	JoinLookp JoinKind = "lookup"
)

// AllJoinKinds lists every supported JoinKind.
var AllJoinKinds = []JoinKind{
	JoinInner, JoinLeft, JoinRight, JoinOuter,
	JoinAnti, JoinSemi, JoinCross, JoinKNN, JoinLookp,
}

// IsKnown reports whether the JoinKind is supported.
func (j JoinKind) IsKnown() bool {
	for _, candidate := range AllJoinKinds {
		if j == candidate {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Per-kind configs. Each is decoded via Node.UnmarshalConfig(&cfg).
// ---------------------------------------------------------------------------

// DatasetInputConfig points at a dataset RID + branch to read from.
type DatasetInputConfig struct {
	DatasetRID string `json:"dataset_rid"`
	Branch     string `json:"branch,omitempty"`
}

// DatasetOutputConfig describes the dataset transaction to commit.
type DatasetOutputConfig struct {
	DatasetRID string `json:"dataset_rid"`
	Branch     string `json:"branch,omitempty"`
	WriteMode  string `json:"write_mode,omitempty"` // SNAPSHOT|APPEND|UPDATE|DELETE
}

// FilterConfig is one Pipeline Builder expression evaluated row-wise.
type FilterConfig struct {
	Predicate string `json:"predicate"`
}

// SelectConfig drops every column outside `columns`. To rename, use
// DerivedColumnConfig.
type SelectConfig struct {
	Columns []string `json:"columns"`
}

// DerivedColumnConfig defines one new column per entry. Existing
// columns may be overwritten when Name collides.
type DerivedColumnConfig struct {
	Columns []DerivedColumn `json:"columns"`
}

// DerivedColumn is one (name, expression) pair.
type DerivedColumn struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
}

// CastConfig coerces each named column to a new type. Type strings
// match pipelineexpression.PipelineType.MarshalJSON output.
type CastConfig struct {
	Casts []ColumnCast `json:"casts"`
}

// ColumnCast is one (column, target_type) pair.
type ColumnCast struct {
	Column     string `json:"column"`
	TargetType string `json:"target_type"`
}

// JoinOnSpec is one equi-join condition: left.<left_column> = right.<right_column>.
type JoinOnSpec struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}

// JoinConfig parametrises every join kind. KNN + Lookup have extra
// knobs (KNNNeighbours, LookupColumn) — kept optional so the JSON
// shape is one struct.
type JoinConfig struct {
	Kind          JoinKind     `json:"kind"`
	On            []JoinOnSpec `json:"on,omitempty"`
	KNNNeighbors int          `json:"knn_neighbors,omitempty"`
	LookupColumn  string       `json:"lookup_column,omitempty"`
}

// AggregateConfig is the standard `GROUP BY` projection.
type AggregateConfig struct {
	GroupBy      []string             `json:"group_by"`
	Aggregations []AggregateProjection `json:"aggregations"`
}

// AggregateProjection is one aggregation in the SELECT list.
type AggregateProjection struct {
	Name       string `json:"name"`
	Function   string `json:"function"` // SUM|AVG|COUNT|MIN|MAX|...
	Expression string `json:"expression,omitempty"`
}

// WindowConfig is shared by aggregate_over_window and
// project_over_window — partition/order/frame are identical.
type WindowConfig struct {
	PartitionBy []string              `json:"partition_by"`
	OrderBy     []OrderByClause       `json:"order_by,omitempty"`
	Frame       *WindowFrame          `json:"frame,omitempty"`
	Outputs     []AggregateProjection `json:"outputs"`
}

// OrderByClause is one column with an explicit asc/desc + nulls
// ordering.
type OrderByClause struct {
	Column    string `json:"column"`
	Direction string `json:"direction,omitempty"`     // asc|desc
	NullsLast bool   `json:"nulls_last,omitempty"`
}

// WindowFrame describes the rows-between / range-between window.
type WindowFrame struct {
	Type  string `json:"type"`  // rows|range
	Start string `json:"start"` // unbounded_preceding | <int> preceding | current_row
	End   string `json:"end"`   // current_row | <int> following | unbounded_following
}

// PivotConfig pivots a long table wide.
type PivotConfig struct {
	PivotColumn string                `json:"pivot_column"`
	ValueColumn string                `json:"value_column"`
	GroupBy     []string              `json:"group_by,omitempty"`
	Aggregations []AggregateProjection `json:"aggregations,omitempty"`
}

// UnpivotConfig unpivots a wide table long.
type UnpivotConfig struct {
	IDColumns      []string `json:"id_columns"`
	ValueColumns   []string `json:"value_columns"`
	NameColumn     string   `json:"name_column"`     // emitted column name
	ValueColumnOut string   `json:"value_column_out"`// emitted value column name
}

// SortConfig sorts the upstream by the given columns.
type SortConfig struct {
	OrderBy []OrderByClause `json:"order_by"`
}

// RankConfig assigns a rank within each partition.
type RankConfig struct {
	PartitionBy []string        `json:"partition_by,omitempty"`
	OrderBy     []OrderByClause `json:"order_by"`
	OutputName  string          `json:"output_name"`
	Function    string          `json:"function,omitempty"` // row_number|rank|dense_rank|percent_rank
}

// CheckpointConfig writes the upstream to a staging dataset so
// downstream nodes can read it without re-executing the pipeline.
type CheckpointConfig struct {
	DatasetRID string `json:"dataset_rid"`
	Branch     string `json:"branch,omitempty"`
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

// UnmarshalConfig decodes Node.Config into target. Returns an error
// when the config is missing or doesn't match the supplied struct.
// Callers should pass the kind-appropriate config type (e.g.
// *FilterConfig for NodeFilter).
func (n Node) UnmarshalConfig(target any) error {
	if len(n.Config) == 0 {
		return fmt.Errorf("node %s: missing config for kind %s", n.ID, n.Kind)
	}
	if err := json.Unmarshal(n.Config, target); err != nil {
		return fmt.Errorf("node %s: decode %s config: %w", n.ID, n.Kind, err)
	}
	return nil
}

// ValidationError is one structural problem with a graph.
type ValidationError struct {
	NodeID  string `json:"node_id,omitempty"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string {
	if e.NodeID == "" {
		return e.Message
	}
	return e.NodeID + ": " + e.Message
}

// Validate runs structural checks against the graph:
//   - Node IDs are non-empty and unique
//   - Every Inputs reference resolves to a node in the graph
//   - Every NodeKind is in AllNodeKinds
//   - Input kinds have no Inputs; output kinds have at least one
//   - No cycles
//   - Every Edge.From / Edge.To references a node (when edges are present)
//
// Per-kind config shape is NOT validated here — that's the compiler's
// job in B2. The point of this function is "is the graph well-formed
// enough to traverse".
func Validate(g Graph) []ValidationError {
	var errs []ValidationError
	if len(g.Nodes) == 0 {
		return append(errs, ValidationError{Message: "graph has no nodes"})
	}

	seenIDs := make(map[string]int, len(g.Nodes))
	for i, node := range g.Nodes {
		if strings.TrimSpace(node.ID) == "" {
			errs = append(errs, ValidationError{Message: fmt.Sprintf("node[%d] has empty id", i)})
			continue
		}
		if _, dup := seenIDs[node.ID]; dup {
			errs = append(errs, ValidationError{NodeID: node.ID, Message: "duplicate id"})
		}
		seenIDs[node.ID] = i

		if !node.Kind.IsKnown() {
			errs = append(errs, ValidationError{NodeID: node.ID, Message: fmt.Sprintf("unknown kind %q", node.Kind)})
			continue
		}

		if node.Kind.IsInput() && len(node.Inputs) > 0 {
			errs = append(errs, ValidationError{NodeID: node.ID, Message: "input nodes cannot have Inputs"})
		}
		if !node.Kind.IsInput() && !node.Kind.IsOutput() && len(node.Inputs) == 0 {
			errs = append(errs, ValidationError{NodeID: node.ID, Message: "transform nodes require at least one input"})
		}
		if node.Kind.IsOutput() && len(node.Inputs) == 0 {
			errs = append(errs, ValidationError{NodeID: node.ID, Message: "output nodes require exactly one input"})
		}
	}

	// Resolve every Inputs reference.
	for _, node := range g.Nodes {
		for _, input := range node.Inputs {
			if _, ok := seenIDs[input]; !ok {
				errs = append(errs, ValidationError{
					NodeID:  node.ID,
					Message: fmt.Sprintf("unknown input reference %q", input),
				})
			}
		}
	}

	// Edges, if present, must reference nodes too.
	for i, edge := range g.Edges {
		if _, ok := seenIDs[edge.From]; !ok {
			errs = append(errs, ValidationError{Message: fmt.Sprintf("edge[%d]: unknown from-node %q", i, edge.From)})
		}
		if _, ok := seenIDs[edge.To]; !ok {
			errs = append(errs, ValidationError{Message: fmt.Sprintf("edge[%d]: unknown to-node %q", i, edge.To)})
		}
	}

	// Cycle detection via DFS on the Inputs adjacency.
	if cycle := detectCycle(g); cycle != nil {
		errs = append(errs, ValidationError{Message: "cycle detected: " + strings.Join(cycle, " → ")})
	}

	return errs
}

// detectCycle returns the cycle path when one exists, or nil. Uses an
// iterative DFS with three-color marking: 0 unvisited, 1 in-stack,
// 2 done.
func detectCycle(g Graph) []string {
	color := make(map[string]int, len(g.Nodes))
	parent := make(map[string]string, len(g.Nodes))
	// Stable iteration order so the reported cycle path is deterministic.
	ids := make([]string, 0, len(g.Nodes))
	for _, node := range g.Nodes {
		ids = append(ids, node.ID)
	}
	sort.Strings(ids)
	for _, start := range ids {
		if color[start] != 0 {
			continue
		}
		if cycle := dfsCycle(start, g, color, parent); cycle != nil {
			return cycle
		}
	}
	return nil
}

func dfsCycle(start string, g Graph, color map[string]int, parent map[string]string) []string {
	nodesByID := make(map[string]Node, len(g.Nodes))
	for _, node := range g.Nodes {
		nodesByID[node.ID] = node
	}
	type frame struct {
		id     string
		cursor int
	}
	stack := []frame{{id: start}}
	color[start] = 1
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		node := nodesByID[top.id]
		if top.cursor >= len(node.Inputs) {
			color[top.id] = 2
			stack = stack[:len(stack)-1]
			continue
		}
		next := node.Inputs[top.cursor]
		top.cursor++
		switch color[next] {
		case 0:
			parent[next] = top.id
			color[next] = 1
			stack = append(stack, frame{id: next})
		case 1:
			// Back-edge: rebuild the cycle.
			path := []string{next, top.id}
			cur := top.id
			for parent[cur] != "" && parent[cur] != next {
				cur = parent[cur]
				path = append(path, cur)
			}
			path = append(path, next)
			// Reverse for readability (root → … → back to root).
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			return path
		}
	}
	return nil
}
