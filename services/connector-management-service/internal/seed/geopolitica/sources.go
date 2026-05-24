// Per-source SeedConnection builders for the geopolitical PoC.
//
// **Source decisions** — verified against Palantir's public Data
// Connection overview (200+ connectors; categories include JDBC, REST,
// file/S3, streaming, SaaS). For the seven geopolitical feeds:
//
//   GDELT events (15-min CSV)            → `csv` (URL-fetched)
//   GDELT GKG mentions (15-min CSV)      → `csv`
//   ACLED (REST JSON + API key)          → `rest_api`
//   OFAC SDN (daily XML)                 → `rest_api` (format_hint=xml)
//   EU Consolidated (daily XML)          → `rest_api` (format_hint=xml)
//   OpenSanctions (REST JSON-Lines)      → `rest_api`
//   Wikidata (SPARQL HTTP GET)           → `rest_api`
//
// Choosing `rest_api` for the XML feeds reflects the Foundry-native
// pattern: a REST source can return any content-type; the bronze →
// silver transform is responsible for the XML decoder. The seed adds
// a `format_hint` annotation so the downstream pipeline can dispatch
// to the right decoder without guessing.
//
// Each builder reuses `mustEncode` to fail-fast at process start if
// a config literal cannot be marshalled — these are package-level
// constants in spirit even though we materialise them with json.Marshal.

package geopolitica

import (
	"encoding/json"
)

// mustEncode marshals a Go map literal as compact JSON. The seed
// catalog is hand-authored so a panic here means the literal itself
// is malformed — we want the process to refuse to boot.
func mustEncode(v map[string]any) json.RawMessage {
	out, err := json.Marshal(v)
	if err != nil {
		panic("geopolitica seed: malformed config literal: " + err.Error())
	}
	return out
}

// ---- GDELT 2.0 events ------------------------------------------------------

// gdeltEventsSeed registers the GDELT 2.0 events feed.
//
// Upstream: http://data.gdeltproject.org/gdeltv2/lastupdate.txt names
// the current 15-minute drop file (zipped, tab-delimited). The
// declared `url` is the index pointer — the downstream ingest job
// follows the redirect chain, gunzips the payload, and parses the
// 61-column CAMEO event schema.
//
// Format: tab-delimited (TSV) inside a gzip archive. The `csv`
// adapter only understands plain CSV — see DownstreamPipelineDoc for
// the bronze decoder transform that bridges the gap.
func gdeltEventsSeed() SeedConnection {
	return SeedConnection{
		Name:          "GDELT 2.0 events (15-min drop)",
		ConnectorType: "csv",
		Config: mustEncode(map[string]any{
			// `csv` adapter requires `url` or `path`. The lastupdate
			// pointer is harmless to GET — the downstream ingest job
			// is the one that walks it.
			"url": "http://data.gdeltproject.org/gdeltv2/lastupdate.txt",
		}),
		Markings: []string{"MARKING:GDELT-RAW", "MARKING:OPEN-SOURCE"},
		Notes:    "Public-attribution feed; 15-minute cadence; gzip tab-delimited; CAMEO event coding; ~1B records lifetime.",
		DownstreamPipelineDoc: `Bronze: gdelt-event-decoder transform — follow lastupdate.txt → fetch .CSV.zip → gunzip → split by tab → emit raw rows.
Silver: cameo-enrichment transform — decode CAMEO root code + quad classes, geocode the action geo.
Gold:  event-projector — write to Event object type (ontology) with source MARKING:GDELT-RAW carried per row.`,
	}
}

// ---- GDELT 2.0 GKG (Global Knowledge Graph) -------------------------------

// gdeltGkgSeed registers the GDELT GKG mentions feed (themes + actors
// + locations extracted from open-source news). Same cadence and
// container as the events feed; different schema (column 16 = V2.1
// Themes, etc.).
func gdeltGkgSeed() SeedConnection {
	return SeedConnection{
		Name:          "GDELT 2.0 GKG mentions",
		ConnectorType: "csv",
		Config: mustEncode(map[string]any{
			"url": "http://data.gdeltproject.org/gdeltv2/lastupdate.txt",
			// The bronze transform filters lastupdate entries to the
			// `gkg.csv.zip` file; selector is documented here for the
			// transform author, not consumed by the csv adapter.
			"resource_hint": "gkg.csv.zip",
		}),
		Markings: []string{"MARKING:GDELT-RAW", "MARKING:OPEN-SOURCE"},
		Notes:    "GKG = themes + named entities + locations; same cadence as events; different schema.",
		DownstreamPipelineDoc: `Bronze: gdelt-gkg-decoder — same lastupdate.txt indirection, filters for ` + "`gkg.csv.zip`" + `.
Silver: gkg-entity-projector — split themes, persons, organizations, locations into separate ontology-bound silver datasets.
Gold:  newsarticle-linker — write to NewsArticle / Theme object types, link to actors/locations.`,
	}
}

// ---- ACLED -----------------------------------------------------------------

// acledSeed registers the ACLED REST API source. Requires an API key
// from acleddata.com; the key lives in the Authorization header at
// ingest time and is NOT baked into the seed (operators paste it
// when bringing the connection up).
func acledSeed() SeedConnection {
	return SeedConnection{
		Name:          "ACLED conflict events",
		ConnectorType: "rest_api",
		Config: mustEncode(map[string]any{
			"base_url":      "https://api.acleddata.com",
			"resource_path": "/acled/read",
			"resource_name": "acled_events",
			"source_kind":   "rest_resource",
			"query_params": map[string]string{
				// `limit` is required by ACLED; 5000 is the page cap.
				"limit": "5000",
			},
			// API key is configured at runtime — operator-set, not
			// committed to git. `runtime.policy_hint` documents it.
			"runtime": map[string]any{
				"policy_hint": "Set ACLED_API_KEY at deploy time; the ingest sidecar projects it into the Authorization header.",
			},
		}),
		Markings: []string{"MARKING:ACLED-RAW", "MARKING:OPEN-SOURCE"},
		Notes:    "REST JSON; 5000-row pages; API key required; daily cadence is fine for PoC scale.",
		DownstreamPipelineDoc: `Bronze: acled-page-collector — walks ` + "`page=1..N`" + ` until the API returns an empty result.
Silver: acled-event-normaliser — flatten ACLED's nested ` + "`actor1`" + `/` + "`actor2`" + ` shapes into row-per-event.
Gold:  acled-event-projector — write to Event object type; link Actor1/Actor2 to canonical Actor objects via entity-resolution-service.`,
	}
}

// ---- OFAC SDN --------------------------------------------------------------

// ofacSdnSeed registers the OFAC SDN list (US Treasury). XML payload
// is GET'd from a stable URL; the `format_hint` field tells the
// downstream pipeline which decoder to dispatch.
func ofacSdnSeed() SeedConnection {
	return SeedConnection{
		Name:          "OFAC SDN list (US Treasury)",
		ConnectorType: "rest_api",
		Config: mustEncode(map[string]any{
			"base_url":      "https://www.treasury.gov",
			"resource_path": "/ofac/downloads/sdn.xml",
			"resource_name": "ofac_sdn_xml",
			"source_kind":   "rest_resource",
			// Format hint is OpenFoundry convention — the adapter
			// ignores it; the bronze decoder reads it via the
			// IngestSpec's metadata block.
			"format_hint": "xml",
		}),
		Markings: []string{"MARKING:SANCTIONS-LIST", "MARKING:OFAC"},
		Notes:    "Stable URL; daily cadence; OFAC accepts unauthenticated GETs; XML payload.",
		DownstreamPipelineDoc: `Bronze: ofac-sdn-xml-decoder — XPath through sdnList/sdnEntry, emit a row per sdnEntry.
Silver: sanctions-aggregator — normalise sdnEntry fields into the canonical SanctionsEntry schema (program, aliases, addresses).
Gold:  sanctions-actor-projector — link SanctionsEntry to Actor via name + program; mark ` + "`Actor.is_sanctioned=true`" + `.`,
	}
}

// ---- EU Consolidated -------------------------------------------------------

// euConsolidatedSeed registers the EU Consolidated Sanctions list.
// Same XML pattern as OFAC; different URL + namespace.
func euConsolidatedSeed() SeedConnection {
	return SeedConnection{
		Name:          "EU Consolidated sanctions",
		ConnectorType: "rest_api",
		Config: mustEncode(map[string]any{
			// The EU Council publishes the file under a token-gated
			// URL that operators substitute at deploy time. We seed
			// the public landing page so the connection materialises
			// with a sane Domain field — the operator then patches
			// the resource_path with the token URL.
			"base_url":      "https://webgate.ec.europa.eu",
			"resource_path": "/europeaid/fpfis/fpa/sanctions/list_en.xml",
			"resource_name": "eu_consolidated_xml",
			"source_kind":   "rest_resource",
			"format_hint":   "xml",
			"runtime": map[string]any{
				"policy_hint": "Replace resource_path with the operator-tokened URL before the first sync. The token is non-public.",
			},
		}),
		Markings: []string{"MARKING:SANCTIONS-LIST", "MARKING:EU"},
		Notes:    "Daily cadence; token-gated URL operator-supplied; XML payload with different schema than OFAC.",
		DownstreamPipelineDoc: `Bronze: eu-sanctions-xml-decoder — XPath through sanctionEntity entries.
Silver: sanctions-aggregator (shared with OFAC) — converge EU + OFAC + OpenSanctions onto canonical SanctionsEntry.
Gold:  sanctions-actor-projector (shared) — link to Actor; carry MARKING:EU + MARKING:OFAC where the same entity appears in both lists.`,
	}
}

// ---- OpenSanctions ---------------------------------------------------------

// openSanctionsSeed registers the OpenSanctions API. JSON Lines via
// the consolidated dataset endpoint; pagination is offset-based.
func openSanctionsSeed() SeedConnection {
	return SeedConnection{
		Name:          "OpenSanctions consolidated",
		ConnectorType: "rest_api",
		Config: mustEncode(map[string]any{
			"base_url":      "https://api.opensanctions.org",
			"resource_path": "/match/default",
			"resource_name": "opensanctions_consolidated",
			"source_kind":   "rest_resource",
			// OpenSanctions free tier is 500 reqs/day; the ingest
			// pipeline must respect that budget — encoded as a
			// runtime hint, not in the adapter config.
			"runtime": map[string]any{
				"policy_hint": "Free-tier rate limit: 500 req/day. Sized for daily refresh; for higher cadence operators must supply an OPENSANCTIONS_API_KEY.",
			},
			"query_params": map[string]string{
				"limit": "1000",
			},
		}),
		Markings: []string{"MARKING:SANCTIONS-LIST", "MARKING:OPENSANCTIONS"},
		Notes:    "JSON; consolidated list across PEPs + sanctions + watchlists; daily PoC cadence; rate-limit-sensitive.",
		DownstreamPipelineDoc: `Bronze: opensanctions-page-collector — walk offsets, persist raw JSONL.
Silver: sanctions-aggregator (shared) — converge with OFAC + EU; OpenSanctions' canonical IDs become Actor.opensanctions_id.
Gold:  opensanctions-actor-enricher — backfill PEP flags + parent organisations onto Actor.`,
	}
}

// ---- Wikidata --------------------------------------------------------------

// wikidataSeed registers the Wikidata SPARQL endpoint. The query is
// embedded as a `query` URL parameter; downstream pipeline rotates
// the query text per Actor class (Person, Organization, ArmedGroup).
func wikidataSeed() SeedConnection {
	// The literal SPARQL is bounded by class — the bronze transform
	// rewrites the actual `query` value at run time. We declare a
	// placeholder that still validates against the SPARQL endpoint
	// schema.
	const placeholderSparql = `SELECT ?item ?itemLabel WHERE { ?item wdt:P31 wd:Q5 . SERVICE wikibase:label { bd:serviceParam wikibase:language "en" . } } LIMIT 1`
	return SeedConnection{
		Name:          "Wikidata SPARQL (geopolitical actors)",
		ConnectorType: "rest_api",
		Config: mustEncode(map[string]any{
			"base_url":      "https://query.wikidata.org",
			"resource_path": "/sparql",
			"resource_name": "wikidata_sparql",
			"source_kind":   "rest_resource",
			"query_params": map[string]string{
				"format": "json",
				"query":  placeholderSparql,
			},
			"headers": map[string]string{
				// Wikidata requires a descriptive UA per their
				// etiquette policy.
				"User-Agent": "OpenFoundry-PoC-Geopolitica/1.0 (https://github.com/openfoundry; contact: openfoundry@example.org)",
			},
			"runtime": map[string]any{
				"policy_hint": "Bronze transform substitutes `query` with class-bound SPARQL (Person/Organization/ArmedGroup/GovernmentBody). Stay under the public-endpoint 5 req/s soft limit.",
			},
		}),
		Markings: []string{"MARKING:WIKIDATA", "MARKING:OPEN-SOURCE"},
		Notes:    "Wikidata SPARQL endpoint; class-bound enrichment; weekly cadence sufficient for PoC.",
		DownstreamPipelineDoc: `Bronze: wikidata-sparql-rotator — issue one query per Actor class with class-bound SPARQL, persist ` + "`itemLabel`" + `, ` + "`item`" + `, country, instance-of, etc.
Silver: wikidata-actor-enricher — flatten the SPARQL result envelope.
Gold:  wikidata-actor-projector — backfill ` + "`Actor.wikidata_qid`" + `, country, type onto canonical Actor; used by entity-resolution-service as a high-trust source.`,
	}
}
