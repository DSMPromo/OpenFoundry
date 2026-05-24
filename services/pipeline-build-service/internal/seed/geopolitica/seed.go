// Package geopolitica declares the bronze → silver → gold pipeline
// transforms for the Geopolitical Intelligence PoC. The 19 transforms
// here mirror the canonical spec at
// PoC/geopolitica/assets/pipeline-transforms.yaml.
//
// Foundry-native framing
// -----------------------
// Pipeline Builder reference:
//
//   - https://www.palantir.com/docs/foundry/pipeline-builder/overview/
//   - https://www.palantir.com/docs/foundry/pipeline-builder/transforms-overview/
//
// Each transform is a node in the pipeline DAG. Foundry's reference
// builds these as expressions + transforms via the canvas editor;
// for source-specific decoders (GDELT TSV, OFAC XML, SPARQL) Foundry
// supports UDFs ("user-defined functions") — that's the path we take
// for OpenFoundry too (transform_kind=udf_go).
//
// Scope of THIS slice
// -------------------
//  1. Register the 19 transforms as declarative metadata so operators
//     see the full DAG in the catalog API + UI surface.
//  2. Wire ONE reference implementation end-to-end (OFAC SDN XML
//     decoder, `libs/poc-geopolitica-transforms/ofac`) so the pattern
//     for the other 18 is proved.
//  3. Document the remaining 18 with clear `Implementation` empty
//     strings + DownstreamPipelineDoc references — reviewers see
//     which transforms are stubs and which are wired.
//
// Each `SeedTransform.Implementation` is the fully-qualified Go
// symbol (or HTTP endpoint, for entity-resolution) the
// pipeline-runner dispatches against when the transform fires. When
// empty, the runner reports `status=not_implemented` and stops the
// DAG at that node — operators can still browse the catalog but the
// run is gated.

package geopolitica

import (
	"context"
	"errors"
	"fmt"
)

// TransformKind discriminates where a transform sits in the
// bronze → silver → gold pipeline.
type TransformKind string

const (
	KindBronzeDecoder   TransformKind = "bronze_decoder"
	KindSilverTransform TransformKind = "silver_transform"
	KindGoldProjector   TransformKind = "gold_projector"
	KindCrossSource     TransformKind = "cross_source"
)

// ExecKind names the implementation strategy — udf_go for pure-Go
// functions, udf_python for Python UDFs (existing path in
// internal/handler/python_transform.go), dsl for plans expressible
// via libs/pipeline-plan ops.
type ExecKind string

const (
	ExecUDFGo     ExecKind = "udf_go"
	ExecUDFPython ExecKind = "udf_python"
	ExecDSL       ExecKind = "dsl"
)

// SeedTransform is one declarative pipeline node. Mirrors the YAML
// spec verbatim; deviations must be reflected in both files.
type SeedTransform struct {
	ID               string
	Kind             TransformKind
	SourceLabel      string
	ExecKind         ExecKind
	ScheduleCron     string
	InputDatasets    []string
	OutputDataset    string
	OutputObjectType string
	OutputMarking    string
	// Implementation is the fully-qualified Go symbol the
	// pipeline-runner dispatches against. Empty = stub.
	Implementation string
	Description    string
}

// IsImplemented reports whether this transform has a wired Go entry
// point. Returns false for stubs — used by the catalog API to badge
// not-yet-wired transforms in the UI.
func (s SeedTransform) IsImplemented() bool {
	return s.Implementation != ""
}

// SeedSink is the minimal surface Load() needs. Production wires this
// to the pipeline-build-service Repo's CreatePipeline + the schedule
// CRUD. Tests inject an in-memory fake.
//
// The seed package does NOT depend on the heavy
// pipeline-build-service internal/postgres types so this package
// stays cheap to import from a future CLI bootstrap.
type SeedSink interface {
	ListRegisteredTransformIDs(ctx context.Context) (map[string]struct{}, error)
	RegisterTransform(ctx context.Context, t SeedTransform) error
}

// LoadResult is what Load returns.
type LoadResult struct {
	Created []string
	Skipped []string
}

// Load applies the catalog against the sink, skipping transforms that
// already have the same id registered (idempotent re-runs).
func Load(ctx context.Context, sink SeedSink) (LoadResult, error) {
	out := LoadResult{}
	if sink == nil {
		return out, errors.New("geopolitica pipeline seed: sink required")
	}
	existing, err := sink.ListRegisteredTransformIDs(ctx)
	if err != nil {
		return out, fmt.Errorf("list registered transforms: %w", err)
	}
	for _, t := range Transforms() {
		if _, present := existing[t.ID]; present {
			out.Skipped = append(out.Skipped, t.ID)
			continue
		}
		if err := sink.RegisterTransform(ctx, t); err != nil {
			return out, fmt.Errorf("register %q: %w", t.ID, err)
		}
		out.Created = append(out.Created, t.ID)
	}
	return out, nil
}

// expectedCount pins the catalog size at startup so a typo in
// Transforms() panics on import instead of shipping a half-set.
const expectedCount = 19

func init() {
	if got := len(Transforms()); got != expectedCount {
		panic(fmt.Sprintf("geopolitica pipeline seed: Transforms() must list %d entries; got %d", expectedCount, got))
	}
}
