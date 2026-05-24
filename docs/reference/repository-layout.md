# Repository Layout

Use this page when you need to quickly answer "where should this change live?"

## Runtime Code

`services/` contains 51 service directories under a single Go module rooted at `github.com/openfoundry/openfoundry-go`. The textual boilerplate every new service starts from lives at `docs/templates/service-skeleton/` (its `.go` files carry `//go:build ignore` so the toolchain skips them in place). Grouping below follows the Helm releases under `infra/helm/apps/` (`of-platform`, `of-data-engine`, `of-ontology`, `of-ml-aip`, `of-apps-ops`, `of-web`).

### Platform (auth, gateway, tenancy)

| Path | What Lives There |
| --- | --- |
| `services/edge-gateway-service` | edge routing, JWT validation, rate limiting, request fan-out |
| `services/identity-federation-service` | login, MFA, WebAuthn, OIDC/SAML/OAuth flows, service accounts, sessions, SCIM, JWKS rotation |
| `services/authorization-policy-service` | Cedar-backed ABAC/RBAC decision point, policy CRUD, restricted views |
| `services/tenancy-organizations-service` | tenant resolution, organizations, workspace enrollments, sharing boundaries |
| `services/audit-compliance-service` | audit ledger, retention policies, lineage deletion subsystem |
| `services/audit-sink` | Kafka → Iceberg consumer for `audit.events.v1` |
| `services/cipher-service` | real same-named backend for the gateway `Cipher` alias (`cipher_service_url`); `DefaultUpstreams()` routes `/api/v1/auth/cipher/*` here by default |
| `services/network-boundary-service` | network-boundary placeholder/API skeleton; the directory exists, but `DefaultUpstreams().NetworkBoundary` still resolves the `network_boundary_service_url` alias to `authorization-policy-service` unless configuration overrides it (the checked-in gateway `config.yaml` does override to port `50119`) |

### Data engine (ingestion, datasets, lineage, pipelines, BI)

| Path | What Lives There |
| --- | --- |
| `services/connector-management-service` | data sources, REST webhooks, connector runtime |
| `services/action-log-sink` | Kafka → Iceberg consumer for ontology action audit events |
| `services/ingestion-replication-service` | batch + streaming ingestion (Kafka via `libs/event-bus-data`), branching, replication |
| `services/dataset-versioning-service` | datasets, branches, transactions, file APIs |
| `services/iceberg-catalog-service` | Iceberg REST catalog (Foundry-flavor) over Lakekeeper |
| `services/iceberg-object-indexer` | Iceberg scan worker that indexes rows into `object-database-service` |
| `services/lineage-service` | OpenLineage sink, lineage graph queries |
| `services/media-sets-service` | media set CRUD, items, branches |
| `services/media-transform-runtime-service` | image / PDF / OCR / geospatial transforms |
| `services/pipeline-build-service` | pipeline authoring + build orchestration |
| `services/pipeline-runner` | Generic pipeline-step runner used by `pipeline-build-service` |
| `services/pipeline-runner-spark` | Scala JAR for Spark transforms (Iceberg read/write) |
| `services/sql-bi-gateway-service` | Apache Arrow Flight SQL server over DataFusion |
| `services/reindex-coordinator-service` | Cassandra reindex coordinator |

### Ontology

| Path | What Lives There |
| --- | --- |
| `services/ontology-definition-service` | object types, properties, link types, action types, function package metadata |
| `services/object-database-service` | object/link storage (Cassandra/Scylla) — write authority |
| `services/ontology-query-service` | read plane: search, graph, object views, KNN, object sets |
| `services/ontology-actions-service` | action validation/planning/execution; also hosts funnels and function metadata |
| `services/function-runtime-service` | user-authored TypeScript/Python function registry and execution runtime |
| `services/ontology-indexer` | Kafka worker projecting ontology changes into the search backend |
| `services/ontology-exploratory-analysis-service` | time-series, geospatial, scenarios |

### ML / AIP

| Path | What Lives There |
| --- | --- |
| `services/model-catalog-service` | model adapter, lifecycle CRUD, experiments |
| `services/model-deployment-service` | model serving runtime adapter |
| `services/agent-runtime-service` | agent runtime API + OpenAI-compatible chat endpoint |
| `services/llm-catalog-service` | LLM provider/model catalog |
| `services/retrieval-context-service` | RAG context retrieval surface; configured as an upstream but not selected by any current `router_table.go` public route |
| `services/knowledge-index-service` | real same-named backend for `knowledge_index_service_url`; current gateway owner for `/api/v1/ai/knowledge-bases*` CRUD, document, and search routes |
| `services/ai-evaluation-service` | LLM evaluation + guardrail benchmarking |
| `services/ai-sink` | Kafka → Iceberg consumer for `ai.events.v1` |

### Apps & ops

| Path | What Lives There |
| --- | --- |
| `services/application-composition-service` | Workshop app composition, pages, widgets, publish runtime |
| `services/notebook-runtime-service` | notebooks: CRUD, cells, sessions, kernels, export |
| `services/workflow-automation-service` | workflow definitions, sagas, automation conditions, **approval steps** |
| `services/notification-alerting-service` | notifications inbox, delivery, WebSocket fan-out |
| `services/telemetry-governance-service` | telemetry permissions, export policies, monitoring rules |
| `services/federation-product-exchange-service` | marketplace, product distribution, federation registry (Nexus capability) |
| `services/code-repository-review-service` | code-security scanning and code review plane |
| `services/global-branch-service` | real same-named backend for `global_branch_service_url` and `/api/v1/global-branches`; legacy `/api/v1/code-repos/.../branches` remains on `code-repository-review-service` |
| `services/sdk-generation-service` | SDK + OpenAPI contract generation/publication |
| `services/solution-design-service` | solution design plane |
| `services/entity-resolution-service` | match rules, merge strategies, fuzzy-matching (Fusion) |
| `services/compute-module-service` | compute module resources |
| `services/report-service` | real same-named backend for `report_service_url`; current default backend for the gateway `Report` alias used by `/api/v1/reports*` |

> Older docs referenced services that **do not exist** as binaries in this monorepo (`ontology-service`, `auth-service`, `audit-service`, `data-connector`, `pipeline-service`, `dataset-service`, `ai-service`, `ml-service`, `marketplace-service`, `document-reporting-service`, `fusion-service`, `streaming-service`, `nexus-service`, `dataset-quality-service`, `lineage-deletion-service`, `event-streaming-service`, `data-asset-catalog-service`). Their capabilities are consolidated in the services above. A gateway key named `*_service_url` is not a guarantee that `services/<name>/` exists or is the code default target; verify aliases against `services/edge-gateway-service/internal/config/config.go` and `docs/architecture/services-and-ports.md`. `cipher-service`, `knowledge-index-service`, `global-branch-service`, and `report-service` do exist now and are the current same-named default backends for their gateway aliases.
>
> Current owners for common retired or legacy names:
>
> | Retired / legacy name | Current owner or route-specific owner |
> | --- | --- |
> | `auth-service` | `identity-federation-service` for login/session/user routes; `authorization-policy-service` for role, permission, group, and policy routes |
> | `audit-service` | gateway legacy alias for `audit-compliance-service` |
> | `pipeline-service` | `pipeline-build-service` |
> | `dataset-service`, `data-asset-catalog-service`, `dataset-quality-service` | `dataset-versioning-service` |
> | `ai-service` | gateway legacy alias for `agent-runtime-service`; provider catalog routes use `llm-catalog-service` |
> | `ml-service` | gateway legacy alias for `model-catalog-service`; deployment/batch-prediction routes use `model-deployment-service` |
> | `streaming-service`, `event-streaming-service` | no current gateway branch; ingestion/streaming capabilities live in `ingestion-replication-service` with `libs/event-bus-data` |
> | `nexus-service` | no binary; `nexus_service_url` is a legacy gateway alias that defaults to `tenancy-organizations-service` for `/api/v1/nexus/spaces`, while broader `/api/v1/nexus*` routes select `federation-product-exchange-service` |
> | `ontology-service` | gateway legacy alias for `ontology-definition-service`; runtime ontology routes are split across `ontology-definition-service`, `object-database-service`, `ontology-query-service`, and `ontology-actions-service` |
> | `lineage-deletion-service` | gateway legacy alias for `audit-compliance-service` |
> | `approvals-service` | `workflow-automation-service/internal/approvals` inside `workflow-automation-service` |
> | `automation-operations-service` | `workflow-automation-service/internal/automationoperations` inside `workflow-automation-service` |
> | `security-governance-service` | no binary; `security_governance_service_url` is a gateway legacy alias for `authorization-policy-service` |
> | `app-builder-service` | `application-composition-service` |
> | `virtual-table-service` | gateway legacy alias for `connector-management-service` |
> | `document-reporting-service` | no binary; `document_reporting_service_url` is a gateway legacy alias for `notebook-runtime-service` |

## Shared Libraries

`libs/` contains 39 cross-cutting Go packages: `auth-middleware`, `authz-cedar-go` (Cedar engine), `audit-trail`, `aws-client` (shared aws-sdk-v2 factory, LocalStack-aware), `core-models`, `db-pool`, `event-bus-control` (NATS JetStream), `event-bus-data` (Kafka), `event-scheduler`, `observability` (slog + OTel + Prometheus), `ontology-kernel`, `pipeline-expression`, `pipeline-plan`, `pipeline-runtime`, `plugin-sdk` (WASM connectors — placeholder), `proto-gen` (generated), `python-sidecar`, `query-engine`, `restrictedview`, `saga`, `scheduling-cron`, `state-machine`, `storage-abstraction`, `testing`, `vector-store`, `ai-kernel-go`, `ml-kernel-go`, `geospatial-core`, `geospatial-tiles`, `cassandra-kernel`, `idempotency`, `outbox`, `media-scanner`, `analytical-logic`, `search-abstraction`, `capabilities`, `scheduling-linter`, `aip-graph-reasoning`, `poc-geopolitica-transforms`.

## UI and Contracts

| Path | Purpose |
| --- | --- |
| `apps/web` | main product frontend (React 19 + Vite + TypeScript) |
| `apps/web/src/routes` | route components (Workshop, ontology, datasets, pipelines, AI, audit, …) |
| `proto/` | 24 Protobuf domains; Go generated to `libs/proto-gen/` |
| `sdks/typescript`, `sdks/python`, `sdks/java` | generated client SDKs |

## Tooling

| Path | Purpose |
| --- | --- |
| `tools/of-cli` | OpenAPI/SDK generation, smoke, benchmarks, mock provider |
| `smoke/scenarios` | scenario-driven smoke definitions |
| `benchmarks/scenarios` | benchmark definitions |
| `Makefile` | canonical task runner (`make tools`, `make ci`, `make gen`, …) |
| `justfile` | thin shim over `make` (Makefile is authoritative) |

## Delivery

| Path | Purpose |
| --- | --- |
| `infra/compose/docker-compose.yml` + `docker-compose.dev.yml` | local infrastructure |
| `infra/helm/infra` | Kubernetes platform layer: third-party charts, operator CRs, bootstrap manifests |
| `infra/helm/operators` | operator charts (cert-manager, CNPG, Flink, K8ssandra, Rook-Ceph, Strimzi, kube-prometheus-stack, Loki, Tempo, OTel Collector, Promtail) |
| `infra/helm/apps` | Kubernetes app layer: `of-platform`, `of-data-engine`, `of-ontology`, `of-ml-aip`, `of-apps-ops`, `of-web` |
| `infra/terraform/providers/openfoundry` | Terraform provider schema output |
| `infra/argocd` | ArgoCD GitOps assets |
| `.github/workflows` | CI/CD pipelines (`openfoundry-go.yml`, `ci-frontend.yml`, `proto-check.yml`, `security-audit.yml`, `chaos-smoke.yml`) |
| `docs/` | technical documentation website (VitePress) |
