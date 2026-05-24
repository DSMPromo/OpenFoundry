package geopolitica

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/poc-geopolitica-transforms/ofac"
)

// --- fake sink --------------------------------------------------------------

type fakeSink struct {
	registered map[string]SeedTransform
	failOn     string
}

func newFakeSink() *fakeSink { return &fakeSink{registered: map[string]SeedTransform{}} }

func (f *fakeSink) ListRegisteredTransformIDs(_ context.Context) (map[string]struct{}, error) {
	out := make(map[string]struct{}, len(f.registered))
	for k := range f.registered {
		out[k] = struct{}{}
	}
	return out, nil
}

func (f *fakeSink) RegisterTransform(_ context.Context, t SeedTransform) error {
	if t.ID == f.failOn {
		return errors.New("forced fake-sink failure for " + t.ID)
	}
	f.registered[t.ID] = t
	return nil
}

// --- catalog shape ----------------------------------------------------------

func TestTransformsCatalogHas19Entries(t *testing.T) {
	got := Transforms()
	require.Len(t, got, expectedCount)
}

func TestEveryTransformDeclaresRequiredFields(t *testing.T) {
	for _, tf := range Transforms() {
		require.NotEmpty(t, tf.ID, "transform missing id")
		require.NotEmpty(t, tf.Kind, "%s missing kind", tf.ID)
		require.NotEmpty(t, tf.SourceLabel, "%s missing source label", tf.ID)
		require.NotEmpty(t, tf.ExecKind, "%s missing exec kind", tf.ID)
		require.NotEmpty(t, tf.OutputDataset, "%s missing output dataset", tf.ID)
		require.NotEmpty(t, tf.Description, "%s missing description", tf.ID)

		// Bronze decoders carry markings; cross-source aggregators
		// don't (they converge marked inputs).
		if tf.Kind == KindBronzeDecoder {
			require.NotEmpty(t, tf.OutputMarking, "%s bronze decoder must carry output marking", tf.ID)
		}
	}
}

func TestTransformIDsAreUnique(t *testing.T) {
	seen := map[string]struct{}{}
	for _, tf := range Transforms() {
		_, dup := seen[tf.ID]
		require.False(t, dup, "duplicate transform id %q", tf.ID)
		seen[tf.ID] = struct{}{}
	}
}

func TestCatalogContainsExpectedTransformKinds(t *testing.T) {
	counts := map[TransformKind]int{}
	for _, tf := range Transforms() {
		counts[tf.Kind]++
	}
	require.Equal(t, 7, counts[KindBronzeDecoder],
		"7 bronze decoders: GDELT events + GDELT GKG + ACLED + OFAC + EU + OpenSanctions + Wikidata")
	require.Equal(t, 4, counts[KindSilverTransform],
		"4 silver transforms: cameo + gkg + acled-normaliser + wikidata-enricher (sanctions-aggregator is cross_source)")
	require.Equal(t, 6, counts[KindGoldProjector],
		"6 gold projectors: event + newsarticle + acled-event + opensanctions + wikidata + sanctions-actor")
	require.Equal(t, 2, counts[KindCrossSource],
		"2 cross-source: sanctions-aggregator + actor-entity-resolution")
}

func TestSchedulesAreCronExpressionsAtMinimum5Fields(t *testing.T) {
	for _, tf := range Transforms() {
		require.NotEmpty(t, tf.ScheduleCron, "%s missing schedule_cron", tf.ID)
		// Minimum 5 fields (minute hour day month weekday). Some
		// implementations allow 6+ (with seconds) — we don't pin a
		// max but disallow free-form non-cron strings.
		fields := strings.Fields(tf.ScheduleCron)
		require.GreaterOrEqual(t, len(fields), 5, "%s schedule_cron %q is malformed", tf.ID, tf.ScheduleCron)
	}
}

func TestDatasetReferencesAreInternallyConsistent(t *testing.T) {
	// Every InputDataset of a non-bronze transform must be the
	// OutputDataset of some earlier transform OR a known external
	// dataset (gdelt_lastupdate_raw, ofac_sdn_raw, etc.).
	outputs := map[string]struct{}{}
	for _, tf := range Transforms() {
		outputs[tf.OutputDataset] = struct{}{}
	}
	knownExternal := map[string]struct{}{
		"gdelt_lastupdate_raw": {},
		"ofac_sdn_raw":         {},
		"eu_sanctions_raw":     {},
	}
	for _, tf := range Transforms() {
		for _, input := range tf.InputDatasets {
			_, isProduced := outputs[input]
			_, isExternal := knownExternal[input]
			require.True(t, isProduced || isExternal,
				"transform %s references unknown input dataset %q (not produced upstream + not in known-external list)",
				tf.ID, input)
		}
	}
}

func TestOFACTransformPointsAtRealImplementation(t *testing.T) {
	var found bool
	for _, tf := range Transforms() {
		if tf.ID == "ofac-sdn-xml-decoder" {
			require.Equal(t, OFACSDNDecoderImpl, tf.Implementation,
				"OFAC SDN decoder must reference the canonical Go symbol")
			require.True(t, tf.IsImplemented(),
				"OFAC SDN decoder is the reference implementation; IsImplemented() must be true")
			found = true
		}
	}
	require.True(t, found)
}

func TestOFACDecoderSymbolMatchesCatalogConstant(t *testing.T) {
	// Smoke-import the package so a rename of DecodeSDN breaks the
	// build instead of letting the catalog drift silently.
	fn := ofac.DecodeSDN
	require.NotNil(t, fn)
	// The constant string and the import path must agree.
	const wantPrefix = "github.com/openfoundry/openfoundry-go/libs/poc-geopolitica-transforms/ofac."
	require.True(t,
		hasPrefix(OFACSDNDecoderImpl, wantPrefix),
		"OFACSDNDecoderImpl=%q does not start with %q", OFACSDNDecoderImpl, wantPrefix)
}

func TestStubsAreClearlyMarked(t *testing.T) {
	// The catalog declares exactly 2 wired implementations
	// (OFAC + ER bridge); the other 17 are stubs whose
	// IsImplemented() must return false.
	wired := 0
	for _, tf := range Transforms() {
		if tf.IsImplemented() {
			wired++
		}
	}
	require.Equal(t, 2, wired, "this slice wires exactly 2 transforms; the rest are stubs")
}

// --- load behaviour ---------------------------------------------------------

func TestLoadOnEmptySinkCreatesAll19(t *testing.T) {
	sink := newFakeSink()
	out, err := Load(context.Background(), sink)
	require.NoError(t, err)
	require.Len(t, out.Created, expectedCount)
	require.Empty(t, out.Skipped)
	require.Len(t, sink.registered, expectedCount)
}

func TestLoadIsIdempotentOnSecondRun(t *testing.T) {
	sink := newFakeSink()
	_, err := Load(context.Background(), sink)
	require.NoError(t, err)
	out, err := Load(context.Background(), sink)
	require.NoError(t, err)
	require.Empty(t, out.Created)
	require.Len(t, out.Skipped, expectedCount)
}

func TestLoadShortCircuitsOnRegisterError(t *testing.T) {
	sink := newFakeSink()
	sink.failOn = "acled-page-collector"
	_, err := Load(context.Background(), sink)
	require.Error(t, err)
	require.Less(t, len(sink.registered), expectedCount,
		"load must short-circuit, not silently keep registering")
}

func TestLoadRejectsNilSink(t *testing.T) {
	_, err := Load(context.Background(), nil)
	require.Error(t, err)
}

// --- helpers ----------------------------------------------------------------

func hasPrefix(s, prefix string) bool { return len(s) >= len(prefix) && s[:len(prefix)] == prefix }
