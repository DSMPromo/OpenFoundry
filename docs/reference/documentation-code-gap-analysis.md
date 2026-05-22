# Documentation vs Code Gap Analysis

This page is a code-first audit of technical documentation drift. The repository rule is: **documentation follows the code; code is never changed just to satisfy stale documentation**.

## Audit Snapshot

Snapshot date: 2026-05-18. The values below come from the working tree, not from roadmap text or archived migration plans.

| Evidence command / source | Current code value | Documentation impact |
| --- | ---: | --- |
| `find services -mindepth 1 -maxdepth 1 -type d` | 51 service directories | Pages must not claim the current monorepo has 42 service binaries. If they mention “42,” they are historical or stale. |
| `find libs -mindepth 1 -maxdepth 1 -type d` | 38 library directories | Pages must not claim the current monorepo has 33 shared libraries. |
| `find proto -mindepth 1 -maxdepth 1 -type d` | 24 protobuf domains | Contract docs should describe these proto domains as the current source of truth. |
| `services/edge-gateway-service/internal/proxy/router_table.go` | Gateway route ownership is prefix-based and alias-driven | Public-route docs must follow the router table, not inferred service names. |
| `services/edge-gateway-service/internal/config/config.go` + `services/edge-gateway-service/config.yaml` + service-local configs | Several gateway aliases intentionally point at consolidated owners instead of same-named placeholder services | Port and route docs must distinguish “binary exists” from “gateway default routes traffic there.” |
| Root `justfile` | `just` is a shim over Makefile targets; no `infra-up`, `dev-stack`, `docs-build`, `smoke`, or `ci-frontend` recipes are defined | Contributor docs must prefer active `make`, `pnpm`, and `docker compose` commands. |

## High-Priority Gaps Found

### 1. Service and library inventory drift

The current filesystem inventory is 51 service directories and 38 shared-library directories. Older repository summaries still claimed 42 Go microservices and 33 shared libraries. The root README has now been corrected to the current counts, and `docs/reference/repository-layout.md` already contains the current detailed inventory.

Services that were historically missing or incorrectly described in older docs include:

- `action-log-sink`
- `cipher-service`
- `function-runtime-service`
- `global-branch-service`
- `iceberg-object-indexer`
- `knowledge-index-service`
- `network-boundary-service`
- `report-service`

Important nuance: several of these are real service directories but still skeletons, placeholders, or route-alias targets in flux. Documentation should state that explicitly instead of omitting them.

Shared libraries that were missing from older “33 libraries” summaries include:

- `pipeline-plan`
- `pipeline-runtime`
- `restrictedview`

### 2. Port tables and service-local READMEs can drift independently

`docs/architecture/services-and-ports.md` now reflects the current service map for the ports that were previously stale. One service-local README was still stale during this audit: `services/authorization-policy-service/README.md` documented `PORT=50115`, while code defaults to `50093`. The README has now been corrected.

Known port/value examples to verify whenever these pages are edited:

| Service | Stale doc value seen in older docs | Current code value / nuance |
| --- | --- | --- |
| `authorization-policy-service` | `50115` | `50093` in `internal/config/config.go` |
| `ingestion-replication-service` | `50120` | `50090` |
| `dataset-versioning-service` | `50117` | `50078` |
| `media-sets-service` | `50121` / `50122` | `50156` / `50157` |
| `ontology-definition-service` | `50122` | `50103` |
| `application-composition-service` | `50118` | `50140` |
| `federation-product-exchange-service` | `50126` | `50120` |
| `iceberg-catalog-service` | `8197` only | Service default is `50118` from `internal/config/config.go`; `DefaultUpstreams()` also uses `50118`; checked-in gateway YAML default for `iceberg_catalog_service_url` is `8197` |

### 3. Gateway alias docs must not equate a service directory with the live route target

The edge gateway keeps legacy upstream fields for Helm/strangler compatibility, but code defaults often point those fields at surviving consolidated owners. Documentation should always present two separate facts:

1. the filesystem/binary inventory under `services/`; and
2. the gateway's current default route target.

Reviewed high-risk aliases:

| Gateway key | Default owner from `DefaultUpstreams()` | Alias status / directory nuance |
| --- | --- | --- |
| `security_governance_service_url` | `authorization-policy-service` | Legacy alias; no `services/security-governance-service` binary. |
| `cipher_service_url` | `cipher-service` | Gateway alias to a real same-named binary; the directory exists and is the default target. |
| `network_boundary_service_url` | `authorization-policy-service` | Legacy alias in code defaults; `services/network-boundary-service` exists, but the Go default does not target it unless config overrides it. The checked-in `config.yaml` override points to `http://localhost:50119`. |
| `checkpoints_purpose_service_url` | `authorization-policy-service` | Legacy alias; no same-named Go service. |
| `data_asset_catalog_service_url` | `dataset-versioning-service` | Legacy alias for the absorbed catalog surface; no `services/data-asset-catalog-service` binary. |
| `dataset_quality_service_url` | `dataset-versioning-service` | Legacy alias for the absorbed quality/health surface; no `services/dataset-quality-service` binary. |
| `application_curation_service_url` | `application-composition-service` | Legacy alias for absorbed app curation/builder surfaces. |
| `knowledge_index_service_url` | `knowledge-index-service` | Gateway key for a real same-named binary; `router_table.go` sends `/api/v1/ai/knowledge-bases*` CRUD, documents, and search here. |
| `document_reporting_service_url` | `notebook-runtime-service` | Legacy alias for absorbed document-reporting surface; no `services/document-reporting-service` binary. |
| `report_service_url` | `report-service` | Gateway alias to a real same-named binary; older notes pointing it at `notebook-runtime-service` are stale. |
| `global_branch_service_url` | `global-branch-service` | Gateway alias to a real same-named binary for `/api/v1/global-branches`; legacy `/api/v1/code-repos/.../branches` stays on `code_repo_service_url`. |
| `marketplace_catalog_service_url` | `federation-product-exchange-service` | Legacy alias for absorbed marketplace catalog surface. |
| `product_distribution_service_url` | `federation-product-exchange-service` | Legacy alias for absorbed product distribution surface. |
| `nexus_service_url` | `tenancy-organizations-service` | Legacy alias for Nexus spaces; broader `/api/v1/nexus*` route ownership is documented separately in `router_table.go` / `services-and-ports.md`. No `services/nexus-service` binary. |

### 4. Route ownership had at least one stale ontology assumption

Older docs said `/api/v1/ontology/types/{id}/objects/query` belonged to `ontology-query-service`. The router table sends broad object-instance paths, including object query paths under `/api/v1/ontology/types/*/objects`, to `object-database-service`; KNN remains on `ontology-query-service`. Any page that lists route ownership should be checked against `services/edge-gateway-service/internal/proxy/router_table.go`.

### 5. Public route ownership drift

The 2026-05-18 route-ownership pass re-read `services/edge-gateway-service/internal/proxy/router_table.go` and re-scanned non-archive Markdown for `/api/v1/`, `/api/v2/`, `/iceberg/v1`, and `/v1/iceberg-clients` mentions. Current corrections include:

- `docs/architecture/services-and-ports.md` now states that `/api/v1/auth/cipher` and `/api/v1/reports` are gateway aliases (`Cipher` and `Report`) rather than assuming ownership solely from same-named service directories.
- Knowledge-base gateway routes follow the current router: both `/api/v1/ai/knowledge-bases` and `/api/v1/ai/knowledge-bases/*/search` select `knowledge-index-service`; `retrieval-context-service` remains configured as an upstream but has no current public `SelectUpstream` branch.
- Compass `/api/v1/compass/search` references are marked as having no current edge-gateway route; workspace routes under `/api/v1/workspace*` continue to select `tenancy-organizations-service`.
- Service-local SQL HTTP prefixes such as `/api/v1/warehouse/*` and `/api/v1/tabular/*` are documented as service-local on `sql-bi-gateway-service`; they are not current edge-gateway routes.

### 6. Local-development command docs still need code-first checks

`docs/operations/deployment-modes.md` still named `just infra-up` and `just dev-stack` during this audit. The root `justfile` does not define those recipes; it delegates only to active Makefile targets. That page has now been corrected to use Docker Compose commands from the active `infra/compose/` paths.

When auditing command docs, treat `docs/developer-toolchain/local-workflows.md`, the root `justfile`, `Makefile`, and package manifests as higher authority than older runbooks or archive pages.

### 7. Retired service names must be labeled or mapped to current owners

The 2026-05-18 retired-service-name pass compared the real service list from
`find services -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort` with
non-archive Markdown hits for retired names such as `audit-service`,
`virtual-table-service`, `dataset-quality-service`, `approvals-service`, and
`automation-operations-service`. The remediation rule is: a non-existent name
may stay only when the surrounding sentence says it is historical, retired, or a
gateway legacy alias and names the current owner. The canonical owner mapping is
now in `docs/reference/repository-layout.md`; gateway aliases are cross-checked
against `docs/architecture/services-and-ports.md` and
`services/edge-gateway-service/internal/config/config.go`.

Corrected current-facing mentions in this pass:

- `docs/ontology-building/define-ontologies.md` and
  `docs/ontology-building/object-edits-and-conflict-resolution.md` now name
  `audit-compliance-service`, not the retired `audit-service`.
- `infra/runbooks/disaster-recovery.md`, `infra/runbooks/vespa.md`,
  `infra/runbooks/kafka.md`, `infra/runbooks/cnpg.md`, and
  `sdks/python/openfoundry_transforms/README.md` now use current owners or
  explicitly label the retired name as historical / gateway legacy alias.
- `infra/runbooks/temporal.md`, `infra/helm/apps/of-platform/README.md`,
  `infra/test-tools/chaos/README.md`,
  `infra/test-tools/chaos/foundry-pattern/README.md`, and
  `docs/architecture/foundry-pattern-migration-closing-audit.md` now describe
  `automation-operations-service` and `approvals-service` as retired binaries
  absorbed by `workflow-automation-service` internals.
- `docs/architecture/adr/ADR-0010-cnpg-postgres-operator.md`,
  `ADR-0024-postgres-consolidation.md`, and
  `ADR-0030-service-consolidation-30-targets.md` now label retired example /
  target-boundary names as historical and point readers to current owners.
- `docs/architecture/adr/ADR-0034-datasets-foundry-parity.md` and
  `ADR-0036-builds-foundry-parity.md` now map the retired
  `dataset-quality-service` surface to `dataset-versioning-service`.
- `docs/architecture/adr/ADR-0040-virtual-tables-service.md` is now explicitly
  historical/superseded and states that `virtual-table-service` does not exist
  as a current binary; the gateway alias resolves to `connector-management-service`.
- `docs/architecture/legacy-migrations/automation-operations-service/README.md`,
  `docs/architecture/legacy-migrations/approvals-service/README.md`, and
  `docs/architecture/migration-directory-classification.md` now label retired
  migration paths and name the `workflow-automation-service` internals that own
  the current saga/approval code.

Historical or target-decomposition mentions left intact after review:

- `docs/migration/*-1to1-checklist.md` sections headed “Suggested service
  boundaries” are target decomposition proposals. Two pages that lacked an
  explicit reader note (`foundry-approvals-checkpoints-sensitive-1to1-checklist.md`
  and `foundry-developer-console-endpoints-mcp-1to1-checklist.md`) now state
  that the listed services are target boundaries rather than live binaries;
  pages that already had such notes were left intact. When a row already uses
  `legacy → current-owner` notation, it is intentionally left as migration
  context.
- `docs/architecture/adr/ADR-0037-foundry-pattern-orchestration.md` and
  `ADR-0021-temporal-on-cassandra-go-workers.md` are historical / superseded
  decision records. Their retired names remain only as historical targets /
  absorbed boundary names, with current owners documented in `repository-layout.md`.
- `docs/architecture/legacy-migrations/**` records old migration directories;
  the directory name and content are historical, and current owners are tracked
  in the canonical repository layout.

Pending TODOs: none from this pass. Every current-facing non-archive mention
reviewed either names a real `services/` directory, identifies the name as a
retired/historical binary, or identifies it as a gateway legacy alias with its
current owner.

## Inventory drift remediation

The 2026-05-18 inventory pass reran the required filesystem evidence commands and reviewed the non-archive Markdown matches from:

```bash
find services -mindepth 1 -maxdepth 1 -type d | wc -l  # 50
find libs -mindepth 1 -maxdepth 1 -type d | wc -l      # 36
find proto -mindepth 1 -maxdepth 1 -type d | wc -l     # 23
rg -n "\b42\b|\b33\b|service directories|shared libraries|Go microservices|microservices|protobuf domains|proto domains" README.md ARCHITECTURE.md CONTRIBUTING.md ROADMAP.md docs infra sdks PoC -g '*.md' -g '!docs/archive/**' -g '!docs/node_modules/**'
```

Files reviewed as current inventory surfaces or likely inventory-adjacent prose:

- `README.md`, `ARCHITECTURE.md`, and `docs/reference/repository-layout.md`: already matched the current 51 service directories, 38 shared libraries, and 24 protobuf domains.
- `PoC/02-arquitectura-y-servicios.md`: already labeled itself a demo-scope snapshot and linked to `docs/reference/repository-layout.md` for the authoritative current service/library list.
- `PoC/README.md`: corrected two current-facing PoC index/scope claims from “42” to the code-derived 51 service directories and linked those claims back to `docs/reference/repository-layout.md`.
- `PoC/13-riesgos-y-plan-b.md`: corrected the customer FAQ answer from “42 Go services” to the code-derived 51 service directories and linked it back to `docs/reference/repository-layout.md`.

Claims intentionally left intact after semantic review:

- Numeric IDs such as checklist entries `*.33` / `*.42`, line references, seeds, IP addresses, timestamps, and “42 Postgres tables” are not repository inventory claims.
- `docs/migration/foundry-feature-parity-matrix.md` uses `32 / 42` and `0 / 42` as migration checklist coverage totals, not as the count of current service directories.
- ADRs under `docs/architecture/adr/` that discuss “33 ownership boundaries” or older “95 dirs / 33 boundaries” are historical architecture decisions and target-boundary discussions, not current physical inventory.
- `infra/helm/infra/mimir/README.md` and other architecture prose that mention generic “microservices” do not assert the current repository service count.

No new automation was added in this pass: `tools/check_docs_drift.py` already verifies the canonical inventory pages (`README.md`, `ARCHITECTURE.md`, `CLAUDE.md`, `docs/reference/repository-layout.md`, and this page) against code-derived service, library, and proto counts. The PoC pages are intentionally demo collateral rather than canonical inventory pages, so they were corrected in prose but not added to the canonical drift gate.

## Code-First Documentation Policy

When updating docs, use this precedence order:

1. **Router ownership:** `services/edge-gateway-service/internal/proxy/router_table.go`.
2. **Gateway defaults:** `services/edge-gateway-service/internal/config/config.go` and `services/edge-gateway-service/config.yaml`.
3. **Service default ports:** `services/<service>/internal/config/config.go`; for koanf-based skeleton services, `services/<service>/config.yaml`.
4. **Service role and readiness:** service-local `README.md`, `NOT_IMPLEMENTED_AUDIT.md`, and tests.
5. **Repository inventory:** `find services`, `find libs`, `find proto`, and checked-in package manifests.
6. **Roadmaps/migration checklists/archive pages:** only after the current code has been verified.

## Follow-Up Work Recommended

- Keep `make docs-drift-check` in the local/CI gate; it fails when repository-layout counts disagree with `services/`, `libs/`, or `proto/`, when `services-and-ports.md` route examples disagree with `router_table.go`, or when documented ports disagree with service/gateway config defaults.
- Generate the service inventory table from service-local metadata where possible.
- Review non-archive pages that still mention retired service names and mark each mention as either a compatibility alias or stale text to remove.
- Review migration checklists that state “green” test commands from old commits; those should be dated or converted into current verification instructions.
