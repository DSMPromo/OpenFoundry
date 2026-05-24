// The 19 PoC transforms ordered as they appear in the YAML spec.
// `Implementation` is empty for stubs and points to a Go symbol for
// the OFAC SDN reference implementation.

package geopolitica

// ofacSDNDecoderImpl is the canonical Go entry point for the OFAC SDN
// XML decoder. Kept as a const so the catalog and the dispatcher
// match exactly — drift here is a build error, not a runtime mystery.
const (
	OFACSDNDecoderImpl        = "github.com/openfoundry/openfoundry-go/libs/poc-geopolitica-transforms/ofac.DecodeSDN"
	ActorEntityResolutionImpl = "github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/seed/geopolitica/entityresolution.Run"
)

// Transforms returns a fresh slice of the 19 PoC transforms in the
// canonical load order (bronze → silver → gold per source, then
// cross-source converging transforms). The output_dataset of upstream
// nodes is the input_dataset of the next.
func Transforms() []SeedTransform {
	return []SeedTransform{
		// ── GDELT events ──────────────────────────────────────────
		{
			ID: "gdelt-event-decoder", Kind: KindBronzeDecoder, SourceLabel: "gdelt", ExecKind: ExecUDFGo,
			ScheduleCron:  "*/15 * * * *",
			InputDatasets: []string{"gdelt_lastupdate_raw"}, OutputDataset: "gdelt_events_bronze",
			OutputMarking: "MARKING:GDELT-RAW",
			Description:   "Follow lastupdate.txt → fetch .export.CSV.zip → gunzip → tab parser → bronze 61-col rows.",
		},
		{
			ID: "cameo-enrichment", Kind: KindSilverTransform, SourceLabel: "gdelt", ExecKind: ExecUDFGo,
			ScheduleCron:  "*/15 * * * *",
			InputDatasets: []string{"gdelt_events_bronze"}, OutputDataset: "gdelt_events_silver",
			Description: "Decode CAMEO root code + quad classes + geocode action_geo.",
		},
		{
			ID: "event-projector", Kind: KindGoldProjector, SourceLabel: "gdelt", ExecKind: ExecUDFGo,
			ScheduleCron:  "*/15 * * * *",
			InputDatasets: []string{"gdelt_events_silver"}, OutputDataset: "events_gold",
			OutputObjectType: "Event", OutputMarking: "MARKING:GDELT-RAW",
			Description: "Write to ontology Event object type with source marking per row.",
		},

		// ── GDELT GKG ─────────────────────────────────────────────
		{
			ID: "gdelt-gkg-decoder", Kind: KindBronzeDecoder, SourceLabel: "gdelt", ExecKind: ExecUDFGo,
			ScheduleCron:  "*/15 * * * *",
			InputDatasets: []string{"gdelt_lastupdate_raw"}, OutputDataset: "gdelt_gkg_bronze",
			OutputMarking: "MARKING:GDELT-RAW",
			Description:   "Same lastupdate.txt indirection; filters for gkg.csv.zip.",
		},
		{
			ID: "gkg-entity-projector", Kind: KindSilverTransform, SourceLabel: "gdelt", ExecKind: ExecUDFGo,
			ScheduleCron:  "*/15 * * * *",
			InputDatasets: []string{"gdelt_gkg_bronze"}, OutputDataset: "gkg_entities_silver",
			Description: "Split themes / persons / orgs / locations into separate silver streams.",
		},
		{
			ID: "newsarticle-linker", Kind: KindGoldProjector, SourceLabel: "gdelt", ExecKind: ExecUDFGo,
			ScheduleCron:  "*/15 * * * *",
			InputDatasets: []string{"gkg_entities_silver"}, OutputDataset: "newsarticles_gold",
			OutputObjectType: "NewsArticle",
			Description:      "Write NewsArticle; create MENTIONED_IN links to Actor / Location.",
		},

		// ── ACLED ─────────────────────────────────────────────────
		{
			ID: "acled-page-collector", Kind: KindBronzeDecoder, SourceLabel: "acled", ExecKind: ExecUDFGo,
			ScheduleCron:  "0 6 * * *",
			InputDatasets: []string{}, OutputDataset: "acled_events_bronze",
			OutputMarking: "MARKING:ACLED-RAW",
			Description:   "REST GET /acled/read?page=N until empty; persist raw JSON.",
		},
		{
			ID: "acled-event-normaliser", Kind: KindSilverTransform, SourceLabel: "acled", ExecKind: ExecUDFGo,
			ScheduleCron:  "0 6 * * *",
			InputDatasets: []string{"acled_events_bronze"}, OutputDataset: "acled_events_silver",
			Description: "Flatten ACLED's nested actor1/actor2 shape into row-per-event.",
		},
		{
			ID: "acled-event-projector", Kind: KindGoldProjector, SourceLabel: "acled", ExecKind: ExecUDFGo,
			ScheduleCron:  "0 6 * * *",
			InputDatasets: []string{"acled_events_silver"}, OutputDataset: "events_gold",
			OutputObjectType: "Event", OutputMarking: "MARKING:ACLED-RAW",
			Description: "Append to Event object type; link to Actor via the cross-source ER transform.",
		},

		// ── OFAC SDN (REFERENCE IMPLEMENTATION) ───────────────────
		{
			ID: "ofac-sdn-xml-decoder", Kind: KindBronzeDecoder, SourceLabel: "ofac", ExecKind: ExecUDFGo,
			ScheduleCron:  "0 5 * * *",
			InputDatasets: []string{"ofac_sdn_raw"}, OutputDataset: "ofac_sanctions_bronze",
			OutputMarking:  "MARKING:OFAC",
			Implementation: OFACSDNDecoderImpl,
			Description:    "XPath through sdnList/sdnEntry → row per sdnEntry with name, program, aliases, addresses.",
		},

		// ── EU Consolidated ───────────────────────────────────────
		{
			ID: "eu-sanctions-xml-decoder", Kind: KindBronzeDecoder, SourceLabel: "eu", ExecKind: ExecUDFGo,
			ScheduleCron:  "0 5 * * *",
			InputDatasets: []string{"eu_sanctions_raw"}, OutputDataset: "eu_sanctions_bronze",
			OutputMarking: "MARKING:EU",
			Description:   "XPath through sanctionEntity entries; different schema than OFAC.",
		},

		// ── OpenSanctions ─────────────────────────────────────────
		{
			ID: "opensanctions-page-collector", Kind: KindBronzeDecoder, SourceLabel: "opensanctions", ExecKind: ExecUDFGo,
			ScheduleCron:  "0 5 * * *",
			InputDatasets: []string{}, OutputDataset: "opensanctions_bronze",
			OutputMarking: "MARKING:OPENSANCTIONS",
			Description:   "Walk offsets through /match/default until empty; persist raw JSONL.",
		},
		{
			ID: "opensanctions-actor-enricher", Kind: KindGoldProjector, SourceLabel: "opensanctions", ExecKind: ExecUDFGo,
			ScheduleCron:  "0 5 * * *",
			InputDatasets: []string{"opensanctions_bronze", "actors_gold"}, OutputDataset: "actors_gold",
			OutputObjectType: "Actor",
			Description:      "Backfill PEP flags + parent_organization onto canonical Actor.",
		},

		// ── Wikidata ──────────────────────────────────────────────
		{
			ID: "wikidata-sparql-rotator", Kind: KindBronzeDecoder, SourceLabel: "wikidata", ExecKind: ExecUDFGo,
			ScheduleCron:  "0 3 * * 0", // weekly Sundays at 03:00
			InputDatasets: []string{}, OutputDataset: "wikidata_actors_bronze",
			OutputMarking: "MARKING:WIKIDATA",
			Description:   "1 SPARQL query per Actor class (Person/Org/ArmedGroup/GovernmentBody).",
		},
		{
			ID: "wikidata-actor-enricher", Kind: KindSilverTransform, SourceLabel: "wikidata", ExecKind: ExecUDFGo,
			ScheduleCron:  "0 3 * * 0",
			InputDatasets: []string{"wikidata_actors_bronze"}, OutputDataset: "wikidata_actors_silver",
			Description: "Flatten SPARQL result envelope (?item/?itemLabel/?country).",
		},
		{
			ID: "wikidata-actor-projector", Kind: KindGoldProjector, SourceLabel: "wikidata", ExecKind: ExecUDFGo,
			ScheduleCron:  "0 3 * * 0",
			InputDatasets: []string{"wikidata_actors_silver", "actors_gold"}, OutputDataset: "actors_gold",
			OutputObjectType: "Actor",
			Description:      "Backfill Actor.wikidata_qid + country + type; high-trust source for ER.",
		},

		// ── Cross-source converging transforms ────────────────────
		{
			ID: "sanctions-aggregator", Kind: KindCrossSource, SourceLabel: "shared", ExecKind: ExecUDFGo,
			ScheduleCron:  "30 5 * * *",
			InputDatasets: []string{"ofac_sanctions_bronze", "eu_sanctions_bronze", "opensanctions_bronze"},
			OutputDataset: "sanctions_silver",
			Description:   "Converge OFAC + EU + OpenSanctions into canonical SanctionsEntry schema.",
		},
		{
			ID: "sanctions-actor-projector", Kind: KindGoldProjector, SourceLabel: "shared", ExecKind: ExecUDFGo,
			ScheduleCron:  "0 6 * * *",
			InputDatasets: []string{"sanctions_silver", "actors_gold"}, OutputDataset: "actors_gold",
			OutputObjectType: "Actor",
			Description:      "Link SanctionsEntry to Actor via name + program; set is_sanctioned=true.",
		},
		{
			ID: "actor-entity-resolution", Kind: KindCrossSource, SourceLabel: "shared", ExecKind: ExecUDFGo,
			ScheduleCron:  "30 6 * * *",
			InputDatasets: []string{"acled_events_silver", "opensanctions_bronze", "sanctions_silver", "wikidata_actors_silver"},
			OutputDataset: "actors_gold", OutputObjectType: "Actor",
			Implementation: ActorEntityResolutionImpl,
			Description:    "Invoke entity-resolution-service::RunJob via the bridge cabled in Gap 2.",
		},
	}
}
