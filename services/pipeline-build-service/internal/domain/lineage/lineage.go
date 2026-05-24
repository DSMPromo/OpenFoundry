// Package lineage ports
// `services/pipeline-build-service/src/domain/lineage/mod.rs` —
// dataset/pipeline/workflow lineage graph + impact analysis +
// marking propagation.
//
// **Phase C scope** (in-process algorithms; the DB-backed I/O sits
// in a follow-up):
//
//   - Type surface: LineageNode, LineageGraphEdge, LineageGraph,
//     LineagePathHop, LineageImpactItem, LineageBuildCandidate,
//     LineageImpactAnalysis, NodeKey + NodeKind.
//   - Graph builder (`BuildGraph`) that filters relations through an
//     allow-list and dedupes nodes.
//   - BFS-based connected-component (`CollectConnectedNodes`) and
//     directional path search (`BFSPaths`).
//   - Impact-analysis builder (`BuildImpactAnalysis`) that walks
//     upstream + downstream BFS paths and surfaces buildable
//     candidates.
//   - Marking propagation: `MarkingRank`, `MaxMarkings`,
//     `EffectivePathMarking`, `RequiresMarkingAcknowledgement`,
//     `CanAccessMarking`, `FilterGraphForClaims`,
//     `FilterImpactForClaims`.
//
// The async DB/HTTP wiring (`get_lineage_graph`,
// `sync_workflow_lineage`, `record_lineage`, `record_column_lineage`,
// `propagate_pipeline_runtime_lineage`) belongs to a follow-up that
// pairs with the migrations + the dataset/workflow services. Today
// the graph algorithms are exercised against in-memory inputs the
// real lineage tables produce.
package lineage

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

// ── Type surface ──────────────────────────────────────────────────

// LineageNode mirrors `pub struct LineageNode`.
type LineageNode struct {
	ID       uuid.UUID       `json:"id"`
	Kind     string          `json:"kind"`
	Label    string          `json:"label"`
	Marking  string          `json:"marking"`
	Metadata json.RawMessage `json:"metadata"`
}

// LineageGraphEdge mirrors `pub struct LineageGraphEdge`.
type LineageGraphEdge struct {
	ID               uuid.UUID       `json:"id"`
	Source           uuid.UUID       `json:"source"`
	SourceKind       string          `json:"source_kind"`
	Target           uuid.UUID       `json:"target"`
	TargetKind       string          `json:"target_kind"`
	RelationKind     string          `json:"relation_kind"`
	PipelineID       *uuid.UUID      `json:"pipeline_id,omitempty"`
	WorkflowID       *uuid.UUID      `json:"workflow_id,omitempty"`
	NodeID           *string         `json:"node_id,omitempty"`
	StepID           *string         `json:"step_id,omitempty"`
	EffectiveMarking string          `json:"effective_marking"`
	Metadata         json.RawMessage `json:"metadata"`
}

// LineageGraph mirrors `pub struct LineageGraph`.
type LineageGraph struct {
	Nodes []LineageNode      `json:"nodes"`
	Edges []LineageGraphEdge `json:"edges"`
}

// LineagePathHop mirrors `pub struct LineagePathHop`.
type LineagePathHop struct {
	SourceID         uuid.UUID `json:"source_id"`
	SourceKind       string    `json:"source_kind"`
	TargetID         uuid.UUID `json:"target_id"`
	TargetKind       string    `json:"target_kind"`
	RelationKind     string    `json:"relation_kind"`
	EffectiveMarking string    `json:"effective_marking"`
}

// LineageImpactItem mirrors `pub struct LineageImpactItem`.
type LineageImpactItem struct {
	ID                      uuid.UUID        `json:"id"`
	Kind                    string           `json:"kind"`
	Label                   string           `json:"label"`
	Distance                int              `json:"distance"`
	Marking                 string           `json:"marking"`
	EffectiveMarking        string           `json:"effective_marking"`
	RequiresAcknowledgement bool             `json:"requires_acknowledgement"`
	Metadata                json.RawMessage  `json:"metadata"`
	Path                    []LineagePathHop `json:"path"`
}

// LineageBuildCandidate mirrors `pub struct LineageBuildCandidate`.
type LineageBuildCandidate struct {
	ID                      uuid.UUID       `json:"id"`
	Kind                    string          `json:"kind"`
	Label                   string          `json:"label"`
	Status                  *string         `json:"status,omitempty"`
	Distance                int             `json:"distance"`
	Triggerable             bool            `json:"triggerable"`
	Marking                 string          `json:"marking"`
	EffectiveMarking        string          `json:"effective_marking"`
	RequiresAcknowledgement bool            `json:"requires_acknowledgement"`
	BlockedReason           *string         `json:"blocked_reason,omitempty"`
	Metadata                json.RawMessage `json:"metadata"`
}

// LineageImpactAnalysis mirrors `pub struct LineageImpactAnalysis`.
type LineageImpactAnalysis struct {
	Root              LineageNode             `json:"root"`
	PropagatedMarking string                  `json:"propagated_marking"`
	Upstream          []LineageImpactItem     `json:"upstream"`
	Downstream        []LineageImpactItem     `json:"downstream"`
	BuildCandidates   []LineageBuildCandidate `json:"build_candidates"`
}

// LineageRelationRecord is the in-memory row shape (mirrors the
// private Rust `struct LineageRelationRecord`). The DB-backed loader
// lands in its own follow-up.
type LineageRelationRecord struct {
	ID               uuid.UUID
	SourceID         uuid.UUID
	SourceKind       string
	TargetID         uuid.UUID
	TargetKind       string
	RelationKind     string
	PipelineID       *uuid.UUID
	WorkflowID       *uuid.UUID
	NodeID           *string
	StepID           *string
	EffectiveMarking string
	Metadata         json.RawMessage
	CreatedAt        time.Time
}

// LineageNodeRecord mirrors the private Rust struct.
type LineageNodeRecord struct {
	EntityID   uuid.UUID
	EntityKind string
	Label      string
	Marking    string
	Metadata   json.RawMessage
}

// NodeKind mirrors `enum NodeKind`. Three variants only.
type NodeKind int

const (
	NodeKindDataset NodeKind = iota
	NodeKindPipeline
	NodeKindWorkflow
)

// String mirrors `NodeKind::as_str`.
func (k NodeKind) String() string {
	switch k {
	case NodeKindDataset:
		return "dataset"
	case NodeKindPipeline:
		return "pipeline"
	case NodeKindWorkflow:
		return "workflow"
	}
	return ""
}

// ParseNodeKind mirrors `NodeKind::parse`. Returns ok=false for
// unknown strings (the caller skips the relation).
func ParseNodeKind(value string) (NodeKind, bool) {
	switch value {
	case "dataset":
		return NodeKindDataset, true
	case "pipeline":
		return NodeKindPipeline, true
	case "workflow":
		return NodeKindWorkflow, true
	}
	return 0, false
}

// NodeKey mirrors `struct NodeKey`. Used as the stable lookup key for
// (id, kind) pairs across nodes + relations.
type NodeKey struct {
	ID   uuid.UUID
	Kind NodeKind
}

// ── Marking propagation ───────────────────────────────────────────

// MarkingRank mirrors `fn marking_rank`. The classification ladder
// the lineage cascade walks: public < confidential < pii.
func MarkingRank(marking string) uint8 {
	switch marking {
	case "pii":
		return 2
	case "confidential":
		return 1
	}
	return 0
}

// MaxMarkings mirrors `fn max_markings`. Returns the highest-rank
// marking from the input slice; empty / nil entries are treated as
// "public".
func MaxMarkings(markings []string) string {
	best := "public"
	bestRank := uint8(0)
	for _, m := range markings {
		candidate := m
		if candidate == "" {
			candidate = "public"
		}
		r := MarkingRank(candidate)
		if r > bestRank {
			best = candidate
			bestRank = r
		}
	}
	return best
}

// EffectivePathMarking mirrors `fn effective_path_marking`. Combines
// the leaf node's marking with every hop's effective marking and
// returns the rank-max.
func EffectivePathMarking(nodeMarking string, relationIDs []uuid.UUID, relationIndex map[uuid.UUID]*LineageRelationRecord) string {
	all := []string{nodeMarking}
	for _, id := range relationIDs {
		r, ok := relationIndex[id]
		if !ok {
			continue
		}
		all = append(all, r.EffectiveMarking)
	}
	return MaxMarkings(all)
}

// RequiresMarkingAcknowledgement mirrors
// `fn requires_marking_acknowledgement`. Anything above public
// requires an explicit caller acknowledgement.
func RequiresMarkingAcknowledgement(marking string) bool {
	return MarkingRank(marking) > 0
}

// CanAccessMarking mirrors `fn can_access_marking`. Admins always
// pass; otherwise the caller's `classification_clearance` attribute
// must outrank the marking.
func CanAccessMarking(claims *authmw.Claims, marking string) bool {
	if claims == nil {
		return MarkingRank(marking) == 0
	}
	if claims.HasRole("admin") {
		return true
	}
	return clearanceRank(claims) >= MarkingRank(marking)
}

func clearanceRank(claims *authmw.Claims) uint8 {
	if claims == nil {
		return 0
	}
	v, ok := claims.Attribute("classification_clearance")
	if !ok {
		return 0
	}
	if s, ok := v.(string); ok {
		return MarkingRank(s)
	}
	return 0
}

// ── Graph builder ─────────────────────────────────────────────────

// BuildGraph mirrors `fn build_graph`. Walks every relation,
// optionally filters via the `allowed` allow-list, and produces the
// canonical sorted-by-NodeKey graph the Rust impl emits. When a node
// is referenced by a relation but not present in `nodes`, a
// `synthetic` placeholder is emitted (matches `build_node_view`).
func BuildGraph(
	nodes map[NodeKey]*LineageNodeRecord,
	relations []LineageRelationRecord,
	allowed map[NodeKey]struct{},
) LineageGraph {
	graphNodes := map[NodeKey]LineageNode{}
	graphEdges := []LineageGraphEdge{}

	for _, rel := range relations {
		sourceKind, ok := ParseNodeKind(rel.SourceKind)
		if !ok {
			continue
		}
		targetKind, ok := ParseNodeKind(rel.TargetKind)
		if !ok {
			continue
		}
		sourceKey := NodeKey{ID: rel.SourceID, Kind: sourceKind}
		targetKey := NodeKey{ID: rel.TargetID, Kind: targetKind}

		if allowed != nil {
			if _, ok := allowed[sourceKey]; !ok {
				continue
			}
			if _, ok := allowed[targetKey]; !ok {
				continue
			}
		}

		if _, exists := graphNodes[sourceKey]; !exists {
			graphNodes[sourceKey] = buildNodeView(sourceKey, nodes)
		}
		if _, exists := graphNodes[targetKey]; !exists {
			graphNodes[targetKey] = buildNodeView(targetKey, nodes)
		}

		graphEdges = append(graphEdges, LineageGraphEdge{
			ID:               rel.ID,
			Source:           rel.SourceID,
			SourceKind:       rel.SourceKind,
			Target:           rel.TargetID,
			TargetKind:       rel.TargetKind,
			RelationKind:     rel.RelationKind,
			PipelineID:       rel.PipelineID,
			WorkflowID:       rel.WorkflowID,
			NodeID:           rel.NodeID,
			StepID:           rel.StepID,
			EffectiveMarking: rel.EffectiveMarking,
			Metadata:         rel.Metadata,
		})
	}

	keys := make([]NodeKey, 0, len(graphNodes))
	for k := range graphNodes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Kind != keys[j].Kind {
			return keys[i].Kind < keys[j].Kind
		}
		return keys[i].ID.String() < keys[j].ID.String()
	})
	out := make([]LineageNode, 0, len(keys))
	for _, k := range keys {
		out = append(out, graphNodes[k])
	}
	return LineageGraph{Nodes: out, Edges: graphEdges}
}

// buildNodeView mirrors `fn build_node_view`. Returns the matching
// record when known, or a synthetic placeholder for unknown nodes.
func buildNodeView(key NodeKey, nodes map[NodeKey]*LineageNodeRecord) LineageNode {
	if rec, ok := nodes[key]; ok {
		return LineageNode{
			ID:       rec.EntityID,
			Kind:     rec.EntityKind,
			Label:    rec.Label,
			Marking:  rec.Marking,
			Metadata: rec.Metadata,
		}
	}
	syntheticMeta, _ := json.Marshal(map[string]any{"synthetic": true})
	return LineageNode{
		ID:       key.ID,
		Kind:     key.Kind.String(),
		Label:    syntheticLabel(key.Kind, key.ID),
		Marking:  "public",
		Metadata: syntheticMeta,
	}
}

func syntheticLabel(kind NodeKind, id uuid.UUID) string {
	s := id.String()
	if len(s) >= 8 {
		s = s[:8]
	}
	return kind.String() + " " + s
}

// ── BFS / connectivity ────────────────────────────────────────────

// CollectConnectedNodes mirrors `fn collect_connected_nodes`. Walks
// undirected edges from `root` to gather every node in the same
// connected component.
func CollectConnectedNodes(root NodeKey, relations []LineageRelationRecord) map[NodeKey]struct{} {
	visited := map[NodeKey]struct{}{root: {}}
	queue := []NodeKey{root}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, rel := range relations {
			sourceKind, ok := ParseNodeKind(rel.SourceKind)
			if !ok {
				continue
			}
			targetKind, ok := ParseNodeKind(rel.TargetKind)
			if !ok {
				continue
			}
			source := NodeKey{ID: rel.SourceID, Kind: sourceKind}
			target := NodeKey{ID: rel.TargetID, Kind: targetKind}
			var neighbor *NodeKey
			switch {
			case source == current:
				neighbor = &target
			case target == current:
				neighbor = &source
			}
			if neighbor == nil {
				continue
			}
			if _, seen := visited[*neighbor]; !seen {
				visited[*neighbor] = struct{}{}
				queue = append(queue, *neighbor)
			}
		}
	}
	return visited
}

// Direction discriminates BFS direction. `Outgoing` follows the
// natural relation direction; `Incoming` reverses every edge.
type Direction int

const (
	DirectionIncoming Direction = iota
	DirectionOutgoing
)

// BFSPaths mirrors `fn bfs_paths`. Returns a map from every reachable
// NodeKey to the relation-id chain that reaches it from `root`. The
// `root` itself maps to an empty chain.
func BFSPaths(root NodeKey, relations []LineageRelationRecord, direction Direction) map[NodeKey][]uuid.UUID {
	queue := []NodeKey{root}
	paths := map[NodeKey][]uuid.UUID{root: {}}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		currentPath := paths[current]
		for _, rel := range relations {
			sourceKind, ok := ParseNodeKind(rel.SourceKind)
			if !ok {
				continue
			}
			targetKind, ok := ParseNodeKind(rel.TargetKind)
			if !ok {
				continue
			}
			source := NodeKey{ID: rel.SourceID, Kind: sourceKind}
			target := NodeKey{ID: rel.TargetID, Kind: targetKind}
			var next *NodeKey
			switch direction {
			case DirectionOutgoing:
				if source == current {
					next = &target
				}
			case DirectionIncoming:
				if target == current {
					next = &source
				}
			}
			if next == nil {
				continue
			}
			if _, seen := paths[*next]; seen {
				continue
			}
			nextPath := make([]uuid.UUID, 0, len(currentPath)+1)
			nextPath = append(nextPath, currentPath...)
			nextPath = append(nextPath, rel.ID)
			paths[*next] = nextPath
			queue = append(queue, *next)
		}
	}
	return paths
}

// ── Impact analysis ───────────────────────────────────────────────

// BuildImpactAnalysis mirrors `pub async fn get_lineage_impact_analysis`
// (the in-memory portion). Walks both directions from `root`, builds
// the impact items, derives the build candidates and surfaces the
// propagated marking.
func BuildImpactAnalysis(
	root NodeKey,
	nodes map[NodeKey]*LineageNodeRecord,
	relations []LineageRelationRecord,
) LineageImpactAnalysis {
	rootView := buildNodeView(root, nodes)
	upstreamPaths := BFSPaths(root, relations, DirectionIncoming)
	downstreamPaths := BFSPaths(root, relations, DirectionOutgoing)
	upstream := buildImpactItems(root, upstreamPaths, nodes, relations)
	downstream := buildImpactItems(root, downstreamPaths, nodes, relations)
	candidates := make([]LineageBuildCandidate, 0, len(downstream))
	for i := range downstream {
		candidates = append(candidates, buildCandidate(&downstream[i], nodes))
	}
	propagated := MaxMarkings(append([]string{rootView.Marking}, collectImpactMarkings(upstream, downstream)...))
	return LineageImpactAnalysis{
		Root:              rootView,
		PropagatedMarking: propagated,
		Upstream:          upstream,
		Downstream:        downstream,
		BuildCandidates:   candidates,
	}
}

func collectImpactMarkings(up, down []LineageImpactItem) []string {
	out := make([]string, 0, len(up)+len(down))
	for _, it := range up {
		out = append(out, it.EffectiveMarking)
	}
	for _, it := range down {
		out = append(out, it.EffectiveMarking)
	}
	return out
}

func buildImpactItems(
	root NodeKey,
	paths map[NodeKey][]uuid.UUID,
	nodes map[NodeKey]*LineageNodeRecord,
	relations []LineageRelationRecord,
) []LineageImpactItem {
	relationIndex := map[uuid.UUID]*LineageRelationRecord{}
	for i := range relations {
		relationIndex[relations[i].ID] = &relations[i]
	}
	items := []LineageImpactItem{}
	for nodeKey, relIDs := range paths {
		if nodeKey == root {
			continue
		}
		node := buildNodeView(nodeKey, nodes)
		effective := EffectivePathMarking(node.Marking, relIDs, relationIndex)
		path := make([]LineagePathHop, 0, len(relIDs))
		for _, id := range relIDs {
			rel, ok := relationIndex[id]
			if !ok {
				continue
			}
			path = append(path, LineagePathHop{
				SourceID:         rel.SourceID,
				SourceKind:       rel.SourceKind,
				TargetID:         rel.TargetID,
				TargetKind:       rel.TargetKind,
				RelationKind:     rel.RelationKind,
				EffectiveMarking: rel.EffectiveMarking,
			})
		}
		meta := mergeMetadata(node.Metadata, map[string]any{
			"node_marking":      node.Marking,
			"effective_marking": effective,
		})
		items = append(items, LineageImpactItem{
			ID:                      node.ID,
			Kind:                    node.Kind,
			Label:                   node.Label,
			Distance:                len(relIDs),
			Marking:                 node.Marking,
			EffectiveMarking:        effective,
			RequiresAcknowledgement: RequiresMarkingAcknowledgement(effective),
			Metadata:                meta,
			Path:                    path,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Distance != items[j].Distance {
			return items[i].Distance < items[j].Distance
		}
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		return items[i].Label < items[j].Label
	})
	return items
}

func buildCandidate(item *LineageImpactItem, nodes map[NodeKey]*LineageNodeRecord) LineageBuildCandidate {
	kind, _ := ParseNodeKind(item.Kind)
	key := NodeKey{ID: item.ID, Kind: kind}
	var status *string
	if rec, ok := nodes[key]; ok {
		var meta map[string]json.RawMessage
		_ = json.Unmarshal(rec.Metadata, &meta)
		if v, ok := meta["status"]; ok {
			var s string
			if err := json.Unmarshal(v, &s); err == nil {
				ss := s
				status = &ss
			}
		}
	}
	triggerable := status != nil && *status == "active"
	return LineageBuildCandidate{
		ID:                      item.ID,
		Kind:                    item.Kind,
		Label:                   item.Label,
		Status:                  status,
		Distance:                item.Distance,
		Triggerable:             triggerable,
		Marking:                 item.Marking,
		EffectiveMarking:        item.EffectiveMarking,
		RequiresAcknowledgement: item.RequiresAcknowledgement,
		Metadata:                item.Metadata,
	}
}

func mergeMetadata(base json.RawMessage, overlay map[string]any) json.RawMessage {
	out := map[string]any{}
	if len(base) > 0 {
		_ = json.Unmarshal(base, &out)
	}
	for k, v := range overlay {
		out[k] = v
	}
	encoded, _ := json.Marshal(out)
	return encoded
}

// ── Marking-aware filters ─────────────────────────────────────────

// FilterGraphForClaims mirrors `pub fn filter_graph_for_claims`.
// Drops every node + edge whose effective marking outranks the
// caller's clearance.
func FilterGraphForClaims(graph LineageGraph, claims *authmw.Claims) LineageGraph {
	if claims != nil && claims.HasRole("admin") {
		return graph
	}
	allowed := map[uuid.UUID]struct{}{}
	for _, n := range graph.Nodes {
		if CanAccessMarking(claims, n.Marking) {
			allowed[n.ID] = struct{}{}
		}
	}
	nodes := graph.Nodes[:0]
	for _, n := range graph.Nodes {
		if _, ok := allowed[n.ID]; ok {
			nodes = append(nodes, n)
		}
	}
	edges := []LineageGraphEdge{}
	for _, e := range graph.Edges {
		if _, ok := allowed[e.Source]; !ok {
			continue
		}
		if _, ok := allowed[e.Target]; !ok {
			continue
		}
		if !CanAccessMarking(claims, e.EffectiveMarking) {
			continue
		}
		edges = append(edges, e)
	}
	return LineageGraph{Nodes: nodes, Edges: edges}
}

// FilterImpactForClaims mirrors `pub fn filter_impact_for_claims`.
// Returns an error when the caller cannot even access the root.
func FilterImpactForClaims(impact LineageImpactAnalysis, claims *authmw.Claims) (LineageImpactAnalysis, error) {
	if !CanAccessMarking(claims, impact.Root.Marking) {
		return LineageImpactAnalysis{}, &accessError{}
	}
	if claims != nil && claims.HasRole("admin") {
		return impact, nil
	}
	upstream := []LineageImpactItem{}
	for _, it := range impact.Upstream {
		if CanAccessMarking(claims, it.EffectiveMarking) {
			upstream = append(upstream, it)
		}
	}
	downstream := []LineageImpactItem{}
	allowedIDs := map[uuid.UUID]struct{}{}
	for _, it := range impact.Downstream {
		if CanAccessMarking(claims, it.EffectiveMarking) {
			downstream = append(downstream, it)
			allowedIDs[it.ID] = struct{}{}
		}
	}
	candidates := []LineageBuildCandidate{}
	for _, c := range impact.BuildCandidates {
		if _, ok := allowedIDs[c.ID]; !ok {
			continue
		}
		if !CanAccessMarking(claims, c.EffectiveMarking) {
			continue
		}
		candidates = append(candidates, c)
	}
	return LineageImpactAnalysis{
		Root:              impact.Root,
		PropagatedMarking: impact.PropagatedMarking,
		Upstream:          upstream,
		Downstream:        downstream,
		BuildCandidates:   candidates,
	}, nil
}

type accessError struct{}

func (a *accessError) Error() string { return "forbidden: insufficient classification clearance" }

// ── Tag → marking helper ───────────────────────────────────────────

// MarkingFromDatasetTags mirrors `fn marking_from_dataset_tags`. Used
// by the snapshot ingester when the dataset row carries marking
// hints in its tag list.
func MarkingFromDatasetTags(tags []string) string {
	for _, prefix := range []string{"marking:", "classification:"} {
		for _, tag := range tags {
			if strings.HasPrefix(tag, prefix) {
				if got, ok := normalizeMarking(strings.TrimPrefix(tag, prefix)); ok {
					return got
				}
			}
		}
	}
	for _, tag := range tags {
		if strings.EqualFold(tag, "pii") {
			return "pii"
		}
	}
	for _, tag := range tags {
		if strings.EqualFold(tag, "confidential") {
			return "confidential"
		}
	}
	return "public"
}

func normalizeMarking(value string) (string, bool) {
	switch value {
	case "", "public":
		return "public", true
	case "confidential":
		return "confidential", true
	case "pii":
		return "pii", true
	}
	return "", false
}
