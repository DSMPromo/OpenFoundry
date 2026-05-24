# PoC Online Retail II — Remaining roadmap

> **Audience:** an AI agent picking the work up cold. This document
> carries operational context, decisions already taken, real gotchas
> encountered, verbatim commands, and per-task verifications.
> It is not a summary — it is a manual.
>
> **Last update:** 2026-05-10
> **Base commit:** `fa60eef9` (`feat(poc,services): runtime read-path for Workshop dashboard`)
> **Companion docs:** [README.md](README.md), [PLAN.md](PLAN.md), [RUNTIME-INDEXER.md](RUNTIME-INDEXER.md)

---

## 0 · Current PoC state (what works, what doesn't)

### ✅ Verified via curl / kubectl

- Spark pipeline materialises **4 Iceberg tables** on Ceph RGW via Lakekeeper REST:
  - `lakekeeper.default.online_retail_raw` (1M rows from CSV)
  - `lakekeeper.default.online_retail_clean` (filtered, qty>0, price>0)
  - `lakekeeper.default.online_retail_anomalies` (flagged needs_review)
  - `lakekeeper.default.online_retail_top_customers` (rank by revenue)
- HTTP bridge `/api/v1/ontology/types/{id}/objects` (List/Get/Create/**Patch**) in
  [services/object-database-service/internal/handlers/objects_bridge.go](../../services/object-database-service/internal/handlers/objects_bridge.go)
  responds with the `ObjectInstance` wire shape the SPA expects. PATCH supports
  `{properties, replace, marking}` — the exact contract that `lib/api/ontology.ts:updateObject()`
  calls, and that Workshop actions use to mutate objects.
- App Builder CRUD `/api/v1/apps` (including publish + public/{slug}) in
  [services/application-composition-service/internal/handlers/apps.go](../../services/application-composition-service/internal/handlers/apps.go).
- Ontology properties + link types HTTP routes in
  [services/ontology-definition-service/internal/handlers/properties_links.go](../../services/ontology-definition-service/internal/handlers/properties_links.go).
- App `PoC — Anomaly Review` (slug `poc-anomaly-review`) created and published (version 1).
- Python seeder loads **496 transactions / 351 products / 29 customers** into the stub-mode ObjectStore.

### 🟡 Partially verified in the browser (pass started)

There is a browser pass in flight that already discovered and patched several things — the files
`apps/web/src/routes/apps/WorkshopEditorPage.tsx` and
`tools/online-retail/dashboard-app-definition.json` are modified, not yet committed:

- **Bridge PATCH added** ([objects_bridge.go:138](../../services/object-database-service/internal/handlers/objects_bridge.go#L138)) —
  merges `properties` over the existing payload and bumps the version with
  `expected_version` (LWT-style). It's what the SPA's `updateObject()` expects.
- **Editor hook order fixed** — the early return `if (!app || !activePage)`
  was before a `useEffect`, breaking the rules of React. Moved after.
- **Page navigation in preview** — the editor only showed the first page.
  Added a tablist that lets you navigate between the fixture's 3 pages.
- **Client-side variable filtering** — the WorkshopEditor now supports
  `static_filter` / `static_filters` on variables (`var_anomalies` filters
  by `review_status=needs_review` directly on the client), because there's
  no server-side pushdown yet. See [WorkshopEditorPage.tsx:438](../../apps/web/src/routes/apps/WorkshopEditorPage.tsx#L438) (`applyStaticFilters`).
- **Defensive defaults** — `ObjectSetTitleWidgetView` was receiving undefined
  `variables`/`objectTypes` when the app loaded in preview with partial data;
  now defaults to `[]`.
- **`per_page=200` → `5000`** in widgets that count elements — with client-side
  filtering you need the full set or you count partially.
- **Fixture `var_anomalies` with direct `static_filter`** — no longer
  depends on a separate widget filter. The `static_filter` JSON is:
  ```json
  { "property_name": "review_status", "operator": "equals", "value": "needs_review" }
  ```

> **Important architectural implication:** filtering 5000 rows on the client
> scales up to ~10⁴. At 10⁵ it breaks. The next step is exposing
> `POST /api/v1/ontology/types/{id}/objects/query` on the bridge with a
> filter-spec pushed down to the ObjectStore. The Cassandra kernel already
> supports queries by owner/marking — extending to property-equals is reasonable.

### ⚠️ Pending / stubbed

- `object-database-service` runs with `OF_DEV_STUB_MODE=true` (in-memory). **Data is lost on pod restart.**
- The "indexer" today is a Python script (`tools/online-retail/seed_object_database.py`) that reads from CSV — not from Iceberg.
- **Not verified in the browser** that the WorkshopEditor renders the dashboard. Only the bridge wire via curl.
- Action writeback to the Iceberg audit log is not implemented.
- The bridge returns `total = len(items)`, not the real cardinality (an acceptable lie when you ask for `per_page=5000`).

---

## 1 · Operational environment

### 1.1 Lima k3s cluster

```
NAME          CPUS    MEMORY    DISK     ARCH        ROLE
k3s-master    2       8GiB      20GiB    aarch64     control-plane
k3s-node1     4       8GiB      40GiB    aarch64     worker (+ 30GiB virtio disk for Ceph OSD)
k3s-node2     4       8GiB      40GiB    aarch64     worker (+ 30GiB virtio disk for Ceph OSD)
```

- **kube context:** `default` (the only one; `lima-of-cluster` does not exist).
- **Memory is very tight.** Services scaled to 0 to leave headroom for Spark drivers (1.5GiB+ each).
- **Architecture:** `arm64` / `aarch64` — all images must be built `--platform linux/arm64`.

### 1.2 Internal registry

```
in-cluster:  registry.registry.svc.cluster.local:5000
host:        localhost:30501  (NodePort)
```

Standard build/publish pattern:

```sh
docker buildx build \
  --platform linux/arm64 \
  --build-arg SERVICE_NAME=<svc> \
  --build-arg TARGETOS=linux \
  --build-arg TARGETARCH=arm64 \
  --build-arg VERSION=<x.y.z> \
  -f services/<svc>/Dockerfile \
  -t localhost:30501/<svc>:<tag> \
  --push \
  .
```

### 1.3 Deployment convention

> ⚠️ **CRITICAL:** the container `name` in Deployments is always
> `app`, NOT the service name. So `kubectl set image` requires:
>
> ```sh
> kubectl -n openfoundry set image deploy/<svc> app=registry.registry.svc.cluster.local:5000/<svc>:<tag>
> ```
>
> Passing `<svc>=<image>` fails with "container not found".

### 1.4 Common port-forwards

```sh
# Gateway (required for all external API calls)
kubectl -n openfoundry port-forward svc/edge-gateway-service 18080:8080 &

# Object database directly (required for bulk seed: the gateway is rate-limited)
kubectl -n openfoundry port-forward svc/object-database-service 18081:8080 &

# Identity (only if you'll use the SPA in dev mode with its custom proxy)
kubectl -n openfoundry port-forward svc/identity-federation-service 50088:8080 &
```

### 1.5 PoC credentials

User created via `/api/v1/auth/register`:

| | |
|---|---|
| email | `smoke@openfoundry.local` |
| password | `openfoundry-smoke-password` |
| user UUID | `019e0f20-0297-7afd-bce5-daaa56a339bc` |

Mint a JWT:

```sh
TOKEN=$(curl -s -X POST http://localhost:18080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"smoke@openfoundry.local","password":"openfoundry-smoke-password"}' \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["access_token"])')
echo "$TOKEN" > /tmp/of-jwt
```

### 1.6 PoC model IDs

| Resource | UUID |
|---|---|
| `transaction` (object type) | `678b55fe-db5f-4d3a-bbf2-8cb643af8d32` |
| `product` (object type) | `616c7a42-6522-4f94-b696-ddb056cf9b11` |
| `customer` (object type) | `46e2598c-0d11-4ab2-a4aa-301f3e8fb5a7` |
| `review_anomaly` (action type) | `019e0f02-7d9f-79ca-89c6-8bf7d71b6e22` |
| `mark_resolved` (action type) | `019e0f02-7dac-76c5-b3ea-3accd44b0639` |
| App `poc-anomaly-review` | obtain via `GET /api/v1/apps` |

### 1.7 Per-service layout (uniform)

```
services/<svc>/
  cmd/<svc>/main.go                  ← entrypoint
  internal/server/server.go          ← chi router
  internal/handlers/                 ← HTTP handlers
  internal/repo/repo.go              ← data access (pgx)
  internal/repo/migrations/*.sql     ← goose-style
  internal/models/                   ← wire types
  internal/config/                   ← koanf
  Dockerfile                         ← multi-stage Go → distroless
```

To create a new service: copy [docs/templates/service-skeleton/](../../docs/templates/service-skeleton/) wholesale into `services/<name>/`, rename it (and drop the `//go:build ignore` headers), register it in:
- [infra/helm/apps/](../../infra/helm/apps/) (Helm chart)
- [services/edge-gateway-service/internal/proxy/router_table.go](../../services/edge-gateway-service/internal/proxy/router_table.go) (if it receives external traffic)
- [infra/argocd/apps/](../../infra/argocd/apps/) (GitOps)

### 1.8 Known gotchas (don't waste time rediscovering them)

1. **Apache Arrow + arm64 + JDK17 → SIGSEGV** in the native vectorized parquet reader. Mitigated with:
   ```
   spark.sql.parquet.enableVectorizedReader=false
   spark.sql.iceberg.vectorization.enabled=false
   ```
   The real fix is bumping Iceberg to 1.6+ (see Task 4.4).

2. **Lima `additionalDisks` auto-format themselves** and break Ceph OSDs. You must set `format: false` in `lima.yaml` for the virtio disks earmarked for OSD.

3. **Rook v1.19+ requires Ceph v19.2+ ("squid").** `v18.2.x` fails the health checks with a cryptic message.

4. **Spark Operator passes `driver` as the first arg to the container.** That's why `services/pipeline-runner/Dockerfile` has no `ENTRYPOINT` — the `apache/spark` base image takes control. If you add an ENTRYPOINT, the driver pods will crashloop with "unknown command".

5. **Mock OAuth2 server** (`navikt/mock-oauth2-server`) needs `requestMappings` with `aud=lakekeeper` + `sub=lakekeeper-operator` in its `JSON_CONFIG`. Without that, Lakekeeper rejects with `InvalidAudience`.

6. **auth-middleware Claims schema:** `sub` and `jti` are `uuid.UUID` (not strings). If you forge a JWT by hand, both fields must be valid UUIDs.

7. **The gateway is rate-limited** and trips HTTP 429 on bursts (~50 req in 1s). For bulk seeds use the direct port-forward to `object-database-service:18081`, not the gateway.

8. **The Vite dev server** runs on `55173`, not `5173`. Its proxy sends:
   - `/api/v1/auth` → `127.0.0.1:50088` (identity-federation-service)
   - `/api/v1/users/me` → `127.0.0.1:50088`
   - `/api/v1/data-connection/...` → `127.0.0.1:50088 / 50119`
   - **catch-all `/api`** → `127.0.0.1:8080` (gateway expected at 8080)

   So for the SPA to work you need the gateway port-forwarded to `8080` (not `18080`) **and** the identity service at `50088`.

---

## 2 · Task 0 — Dashboard browser pass

> **Current status:** partially done. The files `WorkshopEditorPage.tsx`,
> `dashboard-app-definition.json`, `objects_bridge.go` and `server.go` already
> have uncommitted patches. What's left is:
> 1. Close the loop: finish the browser pass with the patches applied.
> 2. Validate that "Mark resolved" does the full round-trip (PATCH → bridge → filter re-render).
> 3. Commit the patches.
>
> This remains the top priority — it has not yet been confirmed that
> the 4 widgets render correct data end-to-end.

### 2.1 Goal

Confirm that `/apps/poc-anomaly-review/preview` renders the 4 widgets
(KPIs, chart_pie, chart_xy, object_table) with the **496 transactions /
181 needs_review / 29 customers** already in the ObjectStore.

### 2.2 Prerequisites

```sh
# A · Bring up port-forwards on the ports the SPA expects (NOT 18080)
kubectl -n openfoundry port-forward svc/edge-gateway-service 8080:8080 &
kubectl -n openfoundry port-forward svc/identity-federation-service 50088:8080 &
kubectl -n openfoundry port-forward svc/object-database-service 18081:8080 &

# B · Verify object-database-service is alive in stub mode with data
kubectl -n openfoundry get deploy object-database-service \
  -o jsonpath='{.spec.replicas}{"\n"}'
# should be 1; if 0 → kubectl scale deploy/object-database-service --replicas=1

# C · Verify the seeded data
curl -s "http://localhost:18081/api/v1/ontology/types/678b55fe-db5f-4d3a-bbf2-8cb643af8d32/objects?per_page=5000" \
  | python3 -c 'import json,sys;d=json.load(sys.stdin);print("transactions:",d["total"])'
# Expect 496. If 0:
#   GATEWAY=http://localhost:18081 TOKEN=skip \
#     python3 tools/online-retail/seed_object_database.py --limit 500
```

### 2.3 Bring up the SPA

```sh
pnpm --filter @open-foundry/web dev
# → http://localhost:55173
```

### 2.4 Login

- Open `http://localhost:55173/login`
- Credentials: `smoke@openfoundry.local` / `openfoundry-smoke-password`
- If the login page fails with a 502, the port-forward to `identity-federation-service:50088` is not active.

### 2.5 Open the dashboard

Find the app ID:

```sh
curl -s http://localhost:8080/api/v1/apps -H "Authorization: Bearer $(cat /tmp/of-jwt)" \
  | python3 -m json.tool
```

Navigate to:
- `/apps/<app-id>/preview` (editor mode with preview)
- or `/apps/poc-anomaly-review` (runtime mode — uses GET `/apps/public/{slug}`)

### 2.6 What to verify — widget by widget

The fixture lives in
[tools/online-retail/dashboard-app-definition.json](../../tools/online-retail/dashboard-app-definition.json).
It has 3 pages:

#### `Overview` page
- **Section "KPIs"**: `kpi_title` with `source_variable_id=var_anomalies` should show the **count of transactions with `review_status=needs_review`** (expected: 181).
- **chart_pie** bound to `transaction.country` → distribution by country (mostly United Kingdom).
- **chart_xy** bound to `transaction.invoice_date` × `line_total` → revenue over time.

#### `Anomalies` page
- **filter_list** over `transaction` filtering `review_status=needs_review`.
- **object_table** with columns `invoice`, `quantity`, `unit_price`, `customer_id`, `country` — should show 181 rows.

#### `Customer drilldown` page
- **property_list** with properties of the selected customer.
- **button_group** with two buttons that invoke the action types:
  - `review_anomaly` → `019e0f02-7d9f-79ca-89c6-8bf7d71b6e22`
  - `mark_resolved` → `019e0f02-7dac-76c5-b3ea-3accd44b0639`
- secondary **object_table** bound to `customer` (29 rows).

### 2.7 "Mark resolved" action

> **Note:** the bridge now exposes `PATCH /api/v1/ontology/types/{id}/objects/{object_id}`
> which merges `{properties}` over the existing payload and bumps the version
> with `expected_version` (LWT-style). This is the endpoint the SPA invokes.
> If `ontology-actions-service` is scaled to 0, you can also test the direct
> round-trip:
>
> ```sh
> # Direct PATCH to the bridge (bypassing ontology-actions-service)
> curl -s -X PATCH \
>   "http://localhost:18081/api/v1/ontology/types/678b55fe-db5f-4d3a-bbf2-8cb643af8d32/objects/<object_id>" \
>   -H "Content-Type: application/json" \
>   -d '{"properties":{"review_status":"resolved"}}'
> ```

1. Click on a row in the Anomalies `object_table` → drill-down.
2. Click "Mark resolved" in the button_group.
3. Verify via curl that the row updates:
   ```sh
   curl -s "http://localhost:8080/api/v1/ontology/types/678b55fe-db5f-4d3a-bbf2-8cb643af8d32/objects?per_page=5000" \
     -H "Authorization: Bearer $(cat /tmp/of-jwt)" \
     | python3 -c '
   import json, sys
   from collections import Counter
   d = json.load(sys.stdin)
   c = Counter(r["properties"].get("review_status") for r in d["data"])
   print(dict(c))'
   ```
   The `needs_review` count should drop by 1, and `resolved` should appear with +1.

### 2.8 Debug if the widgets are empty

1. **DevTools → Network** while loading the page. Look for requests to `/api/v1/ontology/types/.../objects`. Verify status 200 and payload.
2. If the response has data but the widget doesn't render it:
   - Compare the bridge wire shape (in
     [objects_bridge.go:30](../../services/object-database-service/internal/handlers/objects_bridge.go#L30))
     against `ObjectInstance` in
     [apps/web/src/lib/api/ontology.ts:67](../../apps/web/src/lib/api/ontology.ts#L67).
     Possibly missing fields: `marking`, `created_by`.
   - The fixture's widget binding may expect a `path` or `field` that doesn't exist in the properties bag.
3. If `ontology-actions-service` returns 404 when executing the action:
   ```sh
   kubectl -n openfoundry scale deploy/ontology-actions-service --replicas=1
   ```
4. If everything looks fine but `Mark resolved` does not mutate the object: verify that the Execute handler actually calls `object-database-service` (there may be a stub that doesn't propagate). Logs:
   ```sh
   kubectl -n openfoundry logs deploy/ontology-actions-service -f
   ```

### 2.9 Success criteria

| Check | Expected result |
|---|---|
| Widgets render with data > 0 | ✅ |
| KPI counts match curl | 181 needs_review / 496 total |
| chart_pie dominated by UK | UK > 80% |
| Click "Mark resolved" → row disappears from Anomalies after refresh | ✅ |

---

## 3 · Task 1 — Real Iceberg → ObjectStore indexer

> **Priority:** high. Replaces the Python seeder — the missing operational
> piece. Full design in [RUNTIME-INDEXER.md](RUNTIME-INDEXER.md).

### 3.1 Architecture

```
        ┌──────────────────────────┐
        │ iceberg-indexer-service  │  Go control plane
        │  POST /runs              │  pg-runtime-config (runs)
        │  GET  /runs[/{id}]       │
        └─────────┬────────────────┘
                  │ applies SparkApplication CR
                  ▼
        ┌──────────────────────────┐
        │ Spark Operator           │
        └─────────┬────────────────┘
                  │ runs
                  ▼
        ┌──────────────────────────┐
        │ pipeline-runner-spark    │  fat JAR (existing)
        │  IcebergToObjectStore    │  + new main class
        │  Indexer                 │
        │                          │
        │  read Iceberg            │  (Lakekeeper REST + Ceph s3a)
        │  foreachPartition row→   │
        │    HTTP PUT object-db    │
        └─────────┬────────────────┘
                  │ HTTP PUT
                  ▼
        ┌──────────────────────────┐
        │ object-database-service  │
        └──────────────────────────┘
```

### 3.2 Subtask 1.1 — Create the Go service `iceberg-indexer-service`

Copy the template:

```sh
cp -r docs/templates/service-skeleton services/iceberg-indexer-service
cd services/iceberg-indexer-service
# Rename paths cmd/template → cmd/iceberg-indexer-service
mv cmd/template cmd/iceberg-indexer-service
# Drop the //go:build ignore headers so the Go toolchain compiles the copy.
find . -name "*.go" -exec sed -i '' '/^\/\/go:build ignore$/,/^$/d' {} \;
# Find/replace `template` → `iceberg-indexer-service` in go files
find . -name "*.go" -exec sed -i '' 's|services/template|services/iceberg-indexer-service|g' {} \;
```

#### DDL — add migration

File: `services/iceberg-indexer-service/internal/repo/migrations/20260601000000_indexer_runs_foundation.sql`

```sql
CREATE TABLE IF NOT EXISTS iceberg_indexer_runs (
    id                UUID PRIMARY KEY,
    table_ref         TEXT NOT NULL,                    -- e.g. lakekeeper.default.online_retail_clean
    target_tenant     TEXT NOT NULL,
    target_type_id    UUID NOT NULL,
    id_column         TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'queued',   -- queued|running|completed|failed
    snapshot_id_low   BIGINT,                           -- initial snapshot watermark (NULL = full scan)
    snapshot_id_high  BIGINT,                           -- final consumed snapshot (filled on complete)
    rows_processed    BIGINT NOT NULL DEFAULT 0,
    spark_app_name    TEXT,                             -- SparkApplication CR name
    started_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at      TIMESTAMPTZ,
    error             TEXT
);

CREATE INDEX IF NOT EXISTS idx_indexer_runs_table_status
  ON iceberg_indexer_runs(table_ref, status);
CREATE INDEX IF NOT EXISTS idx_indexer_runs_started
  ON iceberg_indexer_runs(started_at DESC);
```

Apply via the existing CNPG Postgres (`pg-runtime-config`).

#### Wire types

File: `services/iceberg-indexer-service/internal/models/run.go`

```go
package models

import (
    "time"
    "github.com/google/uuid"
)

type Run struct {
    ID              uuid.UUID  `json:"id"`
    TableRef        string     `json:"table_ref"`
    TargetTenant    string     `json:"target_tenant"`
    TargetTypeID    uuid.UUID  `json:"target_type_id"`
    IDColumn        string     `json:"id_column"`
    Status          string     `json:"status"`
    SnapshotIDLow   *int64     `json:"snapshot_id_low,omitempty"`
    SnapshotIDHigh  *int64     `json:"snapshot_id_high,omitempty"`
    RowsProcessed   int64      `json:"rows_processed"`
    SparkAppName    *string    `json:"spark_app_name,omitempty"`
    StartedAt       time.Time  `json:"started_at"`
    CompletedAt     *time.Time `json:"completed_at,omitempty"`
    Error           *string    `json:"error,omitempty"`
}

type CreateRunRequest struct {
    TableRef       string    `json:"table_ref"`
    TargetTenant   string    `json:"target_tenant"`
    TargetTypeID   uuid.UUID `json:"target_type_id"`
    IDColumn       string    `json:"id_column"`
    SinceSnapshot  *int64    `json:"since_snapshot,omitempty"`
}

type CompleteRunRequest struct {
    RowsProcessed   int64   `json:"rows_processed"`
    SnapshotIDHigh  int64   `json:"snapshot_id_high"`
    Error           *string `json:"error,omitempty"`
}
```

#### HTTP handlers

```
POST /api/v1/iceberg-indexer/runs               → create run + dispatch SparkApplication, returns 202
GET  /api/v1/iceberg-indexer/runs               → list (paginated by updated_at desc)
GET  /api/v1/iceberg-indexer/runs/{id}          → detail
POST /api/v1/iceberg-indexer/runs/{id}/complete → callback from the Spark job (auth: internal token)
```

`POST /runs`:
1. INSERT into `iceberg_indexer_runs` with status='queued'.
2. If `since_snapshot` is null, read the `snapshot_id_high` of the last completed run for that `table_ref` (incremental by default).
3. Render the SparkApplication CR (template below) and apply it via `k8s.io/client-go`.
4. UPDATE status='running' + spark_app_name.

#### SparkApplication dispatcher

Use [k8s.io/client-go](https://github.com/kubernetes/client-go) with `dynamic.Interface` to avoid depending on the CRD type.

```go
gvr := schema.GroupVersionResource{
    Group: "sparkoperator.k8s.io", Version: "v1beta2", Resource: "sparkapplications",
}
unstructured := buildSparkApp(run)  // see template
_, err := dynClient.Resource(gvr).Namespace("openfoundry").
    Create(ctx, unstructured, metav1.CreateOptions{})
```

For a concrete CR reference, see:
[infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml](../../infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml).

### 3.3 Subtask 1.2 — Scala main `IcebergToObjectStoreIndexer`

File: `services/pipeline-runner-spark/src/main/scala/com/openfoundry/indexer/IcebergToObjectStoreIndexer.scala`

```scala
package com.openfoundry.indexer

import org.apache.http.client.methods.HttpPut
import org.apache.http.entity.StringEntity
import org.apache.http.impl.client.HttpClients
import org.apache.spark.sql.{Row, SparkSession}
import scopt.OParser

import scala.util.Try

final case class IndexerArgs(
  sourceTable:       String  = "",
  targetTenant:      String  = "default",
  targetTypeId:      String  = "",
  idColumn:          String  = "",
  objectDatabaseUrl: String  = "http://object-database-service.openfoundry.svc:8080",
  callbackUrl:       String  = "",
  runId:             String  = "",
  sinceSnapshot:     Option[Long] = None,
  internalToken:     String  = "",
  catalog:           String  = "lakekeeper",
  catalogUri:        String  = "",
)

object IcebergToObjectStoreIndexer {
  private val parser = {
    val b = OParser.builder[IndexerArgs]
    import b._
    OParser.sequence(
      programName("iceberg-to-objectstore-indexer"),
      opt[String]("source-table").required().action((v,a) => a.copy(sourceTable = v)),
      opt[String]("target-tenant").action((v,a) => a.copy(targetTenant = v)),
      opt[String]("target-type-id").required().action((v,a) => a.copy(targetTypeId = v)),
      opt[String]("id-column").required().action((v,a) => a.copy(idColumn = v)),
      opt[String]("object-database-url").action((v,a) => a.copy(objectDatabaseUrl = v)),
      opt[String]("callback-url").required().action((v,a) => a.copy(callbackUrl = v)),
      opt[String]("run-id").required().action((v,a) => a.copy(runId = v)),
      opt[Long]("since-snapshot").optional().action((v,a) => a.copy(sinceSnapshot = Some(v))),
      opt[String]("internal-token").action((v,a) => a.copy(internalToken = v)),
      opt[String]("catalog").action((v,a) => a.copy(catalog = v)),
      opt[String]("catalog-uri").action((v,a) => a.copy(catalogUri = v)),
    )
  }

  def main(rawArgs: Array[String]): Unit = {
    val args = OParser.parse(parser, rawArgs, IndexerArgs()).getOrElse {
      System.err.println("[indexer] failed to parse args"); sys.exit(2)
    }
    val spark = buildSession(args)
    try {
      val df = readSource(spark, args)
      val rowsProcessed = df.count()
      // Keep foreachPartition: one HTTP client per partition to avoid open/close churn.
      df.foreachPartition { rows: Iterator[Row] =>
        val client = HttpClients.createDefault()
        rows.foreach { row =>
          val id = row.getAs[Any](args.idColumn).toString
          val payload = rowToJson(row, exclude = Set(args.idColumn))
          val body = s"""{"type_id":"${args.targetTypeId}","version":${snapshotIdOrEpoch(args)},"payload":$payload,"updated_at_ms":${System.currentTimeMillis()}}"""
          val put = new HttpPut(s"${args.objectDatabaseUrl}/api/v1/object-database/objects/${args.targetTenant}/$id")
          put.setHeader("Content-Type", "application/json")
          if (args.internalToken.nonEmpty) put.setHeader("X-Internal-Token", args.internalToken)
          put.setEntity(new StringEntity(body, "UTF-8"))
          client.execute(put).close()
        }
        client.close()
      }
      callback(args, rowsProcessed, snapshotIdOrEpoch(args), None)
    } catch { case t: Throwable =>
      callback(args, 0L, 0L, Some(t.getMessage))
      throw t
    } finally spark.stop()
  }

  private def buildSession(args: IndexerArgs): SparkSession = {
    val b = SparkSession.builder()
      .appName(s"indexer-${args.runId}")
      .config("spark.sql.extensions", "org.apache.iceberg.spark.extensions.IcebergSparkSessionExtensions")
      .config(s"spark.sql.catalog.${args.catalog}", "org.apache.iceberg.spark.SparkCatalog")
      .config(s"spark.sql.catalog.${args.catalog}.type", "rest")
      // workaround Apache Arrow + arm64 + JDK17:
      .config("spark.sql.parquet.enableVectorizedReader", "false")
      .config("spark.sql.iceberg.vectorization.enabled", "false")
    if (args.catalogUri.nonEmpty) b.config(s"spark.sql.catalog.${args.catalog}.uri", args.catalogUri)
    b.getOrCreate()
  }

  private def readSource(spark: SparkSession, args: IndexerArgs) = {
    args.sinceSnapshot match {
      case Some(snap) => spark.read.option("start-snapshot-id", snap.toString).table(args.sourceTable)
      case None       => spark.read.table(args.sourceTable)
    }
  }

  private def rowToJson(row: Row, exclude: Set[String]): String = {
    // Trivial implementation: use row.json (Spark Row has a no-op .json method —
    // better to build it manually with DataFrame.toJSON before the foreachPartition).
    // ALTERNATIVE: convert df → df.toJSON (Dataset[String]) and process strings.
    ???
  }
  // ... callback() + snapshotIdOrEpoch() ...
}
```

> **Recommended refactor:** instead of iterating `Row`s with manual JSON conversion,
> do `df.toJSON.foreachPartition { jsonStrings => ... }`. Spark serialises each row
> to JSON and your code just builds the `{type_id, version, payload, ...}` envelope.

### 3.4 Subtask 1.3 — Build & register the new main class

`services/pipeline-runner-spark/build.sbt` doesn't need any change to add
a second main class — Spark-submit receives `--class
com.openfoundry.indexer.IcebergToObjectStoreIndexer` and the class is
already in the fat JAR.

Build:

```sh
cd services/pipeline-runner-spark
sbt assembly
ls target/scala-2.12/pipeline-runner-spark-dev.jar  # verify
```

Then, build the pipeline-runner base image, which already copies the JAR:

```sh
docker buildx build --platform linux/arm64 \
  --build-arg VERSION=indexer-dev \
  -f services/pipeline-runner/Dockerfile \
  -t localhost:30501/pipeline-runner:indexer-dev --push .
```

### 3.5 Subtask 1.4 — SparkApplication CR template

File: `infra/helm/infra/spark-jobs/templates/_indexer-run-template.yaml`

Copy the existing `_pipeline-run-template.yaml` and change:
- `mainClass` → `com.openfoundry.indexer.IcebergToObjectStoreIndexer`
- `arguments` → the new CLI flags (including `--callback-url` pointing to `iceberg-indexer-service.openfoundry.svc:8080/api/v1/iceberg-indexer/runs/{id}/complete`).
- `serviceAccount: spark` (the RBAC is already applied by `spark-rbac.yaml`).

### 3.6 Subtask 1.5 — Auto-trigger from pipeline-build-service

Decision to make: **is `target_object_type_id` configured on the dataset (metadata) or on the pipeline (config)?**

Recommendation: **on the dataset** (`datasets-service`). It is the natural property of the output, not of the process.

```sql
-- migration in datasets-service
ALTER TABLE datasets ADD COLUMN target_object_type_id UUID;
ALTER TABLE datasets ADD COLUMN target_id_column TEXT;
```

When pipeline-build-service detects that a pipeline run completed, look at the dataset output:
- If it has `target_object_type_id` set → POST `/api/v1/iceberg-indexer/runs` with that data.

### 3.7 Verification

```sh
# Manually trigger a run
curl -s -X POST http://localhost:8080/api/v1/iceberg-indexer/runs \
  -H "Authorization: Bearer $(cat /tmp/of-jwt)" \
  -H "Content-Type: application/json" \
  -d '{
    "table_ref":      "lakekeeper.default.online_retail_clean",
    "target_tenant":  "default",
    "target_type_id": "678b55fe-db5f-4d3a-bbf2-8cb643af8d32",
    "id_column":      "transaction_id"
  }'

# Watch the SparkApplication
kubectl -n openfoundry get sparkapplications.sparkoperator.k8s.io -w

# When it completes
curl -s "http://localhost:18081/api/v1/ontology/types/678b55fe-db5f-4d3a-bbf2-8cb643af8d32/objects?per_page=5"
```

### 3.8 Task 1 gotchas

- **Serial HTTP PUT inside `foreachPartition` is slow** (~5K rows/min). For real scale: implement a new endpoint `POST /api/v1/object-database/objects/bulk` in `object-database-service` that accepts an array.
- **`df.foreachPartition` doesn't return the count.** Materialise `df.count()` BEFORE (caching the DF).
- **The callback must be idempotent** — Spark may retry the job. Use `run_id` as the dedup key in the complete handler.
- **While `object-database-service` is in stub mode, data is lost on restart.** For real tests, complete Task 3 (Cassandra) first.
- **`since-snapshot=null` means full re-scan.** If the table has 100M rows, you'll saturate. Accept an optional `--limit-rows` for the PoC.

---

## 4 · Task 2 — Iceberg writeback for the audit log

> **Priority:** medium. The architectural decision is made
> ([RUNTIME-INDEXER.md § P4](RUNTIME-INDEXER.md#p4--writeback-decision-hybrid-cassandra-canonical--iceberg-audit-log)):
> Cassandra is canonical for the object state; Iceberg is canonical for
> the immutable action log (audit + time-travel).

### 4.1 Producer in `ontology-actions-service`

File: `services/ontology-actions-service/internal/handlers/execute.go` (verify the actual path with `find services/ontology-actions-service -name "*.go" | xargs grep -l "Execute"`).

After a successful Execute (object mutated and 200 returned), publish to Kafka:

```go
type ActionAppliedEvent struct {
    EventID       uuid.UUID `json:"event_id"`
    ActionTypeID  uuid.UUID `json:"action_type_id"`
    ActionName    string    `json:"action_name"`
    ObjectTypeID  uuid.UUID `json:"object_type_id"`
    ObjectID      string    `json:"object_id"`
    Tenant        string    `json:"tenant"`
    ActorSub      uuid.UUID `json:"actor_sub"`
    ActorEmail    string    `json:"actor_email"`
    PreviousState json.RawMessage `json:"previous_state"`
    NewState      json.RawMessage `json:"new_state"`
    AppliedAtMs   int64     `json:"applied_at_ms"`
}

evt := ActionAppliedEvent{ EventID: uuid.New(), ... }
data, _ := json.Marshal(evt)
h.KafkaProducer.WriteMessage(ctx, "ontology.actions.applied.v1", []byte(evt.ObjectID), data)
```

Look up the existing pattern:
```sh
grep -rn "kafka.WriteMessage\|kafka-go\|sarama" services/ libs/event-bus/ 2>/dev/null
```

#### Create the topic

```sh
kubectl -n kafka exec -it openfoundry-kafka-0 -- bin/kafka-topics.sh \
  --bootstrap-server localhost:9092 \
  --create --topic ontology.actions.applied.v1 \
  --partitions 12 --replication-factor 1
```

### 4.2 Spark Structured Streaming consumer

Decision: the consumer is a permanent SparkApplication (mode=streaming), not a batch job.

File: `services/pipeline-runner-spark/src/main/scala/com/openfoundry/audit/ActionLogStreamSink.scala`

```scala
package com.openfoundry.audit

import org.apache.spark.sql.SparkSession
import org.apache.spark.sql.functions.{col, from_json}
import org.apache.spark.sql.types._

object ActionLogStreamSink {
  def main(args: Array[String]): Unit = {
    val spark = SparkSession.builder()
      .appName("action-log-sink")
      .config("spark.sql.extensions", "org.apache.iceberg.spark.extensions.IcebergSparkSessionExtensions")
      .config("spark.sql.catalog.lakekeeper", "org.apache.iceberg.spark.SparkCatalog")
      .config("spark.sql.catalog.lakekeeper.type", "rest")
      .getOrCreate()

    val schema = StructType(Seq(
      StructField("event_id", StringType, nullable = false),
      StructField("action_type_id", StringType, nullable = false),
      StructField("action_name", StringType, nullable = false),
      StructField("object_type_id", StringType, nullable = false),
      StructField("object_id", StringType, nullable = false),
      StructField("tenant", StringType, nullable = false),
      StructField("actor_sub", StringType, nullable = false),
      StructField("actor_email", StringType, nullable = true),
      StructField("previous_state", StringType, nullable = true),
      StructField("new_state", StringType, nullable = true),
      StructField("applied_at_ms", LongType, nullable = false),
    ))

    val df = spark.readStream.format("kafka")
      .option("kafka.bootstrap.servers", "openfoundry-kafka-bootstrap.kafka.svc:9092")
      .option("subscribe", "ontology.actions.applied.v1")
      .option("startingOffsets", "earliest")
      .load()
      .selectExpr("CAST(value AS STRING) AS json", "timestamp AS kafka_ts")
      .select(from_json(col("json"), schema).as("evt"), col("kafka_ts"))
      .select("evt.*", "kafka_ts")

    df.writeStream.format("iceberg")
      .outputMode("append")
      .option("checkpointLocation", "s3a://openfoundry-iceberg/_checkpoints/action_log")
      .trigger(org.apache.spark.sql.streaming.Trigger.ProcessingTime("30 seconds"))
      .toTable("lakekeeper.default.action_log")
      .awaitTermination()
  }
}
```

#### Create the table

```sql
-- via Spark SQL against lakekeeper:
CREATE TABLE IF NOT EXISTS lakekeeper.default.action_log (
  event_id        STRING,
  action_type_id  STRING,
  action_name     STRING,
  object_type_id  STRING,
  object_id       STRING,
  tenant          STRING,
  actor_sub       STRING,
  actor_email     STRING,
  previous_state  STRING,
  new_state       STRING,
  applied_at_ms   BIGINT,
  kafka_ts        TIMESTAMP
)
USING iceberg
PARTITIONED BY (days(from_unixtime(applied_at_ms / 1000)));
```

### 4.3 Verification

```sh
# 1. Consume the Kafka topic while triggering an action from the dashboard
kubectl -n kafka exec -it openfoundry-kafka-0 -- bin/kafka-console-consumer.sh \
  --bootstrap-server localhost:9092 \
  --topic ontology.actions.applied.v1 \
  --from-beginning --max-messages 5

# 2. Time-travel over the audit table
# (can be done via spark-submit with inline SQL)
spark-submit --class com.openfoundry.pipeline.PipelineRunner \
  ... \
  --inline-sql "SELECT * FROM lakekeeper.default.action_log VERSION AS OF 0 LIMIT 10"
```

### 4.4 Task 2 gotchas

- **The consumer must be idempotent.** Kafka is at-least-once; use `event_id` as the dedup key in a `WHERE NOT EXISTS` or `MERGE INTO`.
- **Iceberg streaming writes require snapshot expiry.** Without a weekly `expire_snapshots()` cron the metadata grows without bound. Add a CronJob-style SparkApplication.
- **If the Kafka topic doesn't exist, fail loud.** `ontology-actions-service` must reject the Execute if the publish fails — without the audit log, actions are non-traceable.
- **Schema evolution:** the event JSON may grow. Use `from_json` with an explicit schema and new fields as nullable; never change types.

---

## 5 · Task 3 — Real Cassandra (remove stub mode)

### 5.1 Goal

Replace `OF_DEV_STUB_MODE=true` with a Cassandra cluster that persists across restarts.

### 5.2 Steps

#### 5.2.1 Deploy with K8ssandra-Operator (recommended) or the Bitnami chart

```sh
# Quick option: Bitnami
helm repo add bitnami https://charts.bitnami.com/bitnami
helm install cassandra bitnami/cassandra \
  --namespace cassandra --create-namespace \
  --set cluster.replicaCount=1 \
  --set cluster.datacenter=dc1 \
  --set persistence.size=10Gi \
  --set jvm.maxHeapSize=2G \
  --set jvm.newHeapSize=512M \
  --set image.tag=4.1
```

> ⚠️ **Memory:** Cassandra's default heap is 3Gi. On a Lima VM with 8Gi
> you're already close to the limit with everything else. Overriding to
> a 2G heap is essential.

#### 5.2.2 Schema

`object-database-service` uses `libs/cassandra-kernel`. Find the DDL:

```sh
find services/object-database-service/cql -type f
find libs/cassandra-kernel -name "*.cql"
```

Apply:

```sh
kubectl -n cassandra exec -it cassandra-0 -- cqlsh -f /tmp/schema.cql
```

#### 5.2.3 Configure the service

```sh
kubectl -n openfoundry set env deploy/object-database-service \
  CASSANDRA_CONTACT_POINTS=cassandra.cassandra.svc:9042 \
  CASSANDRA_KEYSPACE_OBJECTS=ontology_objects \
  CASSANDRA_KEYSPACE_LINKS=ontology_links \
  CASSANDRA_LOCAL_DC=dc1

# Remove the stub mode
kubectl -n openfoundry set env deploy/object-database-service OF_DEV_STUB_MODE-
kubectl -n openfoundry set env deploy/object-database-service OBJECT_DATABASE_BACKEND-

kubectl -n openfoundry rollout restart deploy/object-database-service
```

#### 5.2.4 Re-seed

The stub data is gone — re-run the seeder or trigger the Task 1 indexer.

### 5.3 Task 3 gotchas

- **Cassandra arm64:** Bitnami publishes multi-arch from 4.1 onward. Earlier tags fail with `exec format error`.
- **Replication factor 1 in dev:** make sure the keyspace uses `NetworkTopologyStrategy` with `dc1: 1`. The current Go services and local fixtures assume that single-node dev shape.
- **Cassandra start time:** ~60s. The object-database-service readinessProbe must wait for it (otherwise it crashloops forever).
- **Memory:** if Lima OOMkills Cassandra, lower the heap to 1G and accept the latency.

---

## 6 · Task 4 — Quality / operational

### 6.1 ObjectStore.Count

#### Why
The bridge in
[objects_bridge.go:115](../../services/object-database-service/internal/handlers/objects_bridge.go#L115)
returns `total = len(items)`. If you ask for `per_page=10` with 500 real
rows, `total=10` — a lie. The dashboard's KPIs use `total`, so we see fake numbers.

#### Changes

[storage/types.go:127](../../services/object-database-service/internal/storage/types.go#L127):

```go
type ObjectStore interface {
    // ... existing ...
    Count(ctx context.Context, tenant TenantId, typeID TypeId) (uint64, error)
}
```

Implementations:
- **InMemory:** trivial, walk the slice.
- **Cassandra:** `SELECT COUNT(*) FROM objects_by_type WHERE tenant=? AND type_id=?` — expensive at scale. Alternative: keep a denormalised counter in an `object_counts(tenant, type_id, count)` table updated by triggers / batch.

Bridge:
```go
total, err := h.Objects.Count(r.Context(), tenant, typeID)
// use `total` in the response, not len(items)
```

### 6.2 Real pagination tokens

#### Why
[objects_bridge.go:96](../../services/object-database-service/internal/handlers/objects_bridge.go#L96)
discards `next_token`. The in-memory store already supports pagination but the handler doesn't expose it.

#### Changes

```go
// read cursor from the query string
cursor := r.URL.Query().Get("cursor")
var token *string
if cursor != "" { token = &cursor }

res, err := h.Objects.ListByType(r.Context(), tenant, typeID,
    storage.Page{Size: perPage, Token: token}, ...)

// return next_cursor
writeJSON(w, http.StatusOK, map[string]any{
    "data":        items,
    "total":       total,
    "next_cursor": res.NextToken,
    "page":        page,
    "per_page":    perPage,
})
```

Frontend in
[apps/web/src/lib/api/ontology.ts:268](../../apps/web/src/lib/api/ontology.ts#L268)
already expects `{data,total,page,per_page}` — adding `next_cursor` doesn't break anything.

### 6.3 Service-to-service auth

#### Why
[server.go:1-9](../../services/object-database-service/internal/server/server.go#L1-9)
explicitly says "no JWT auth — trust the gateway". If a pod discovers the ClusterIP `10.43.155.122:8080`, it bypasses the gateway.

#### Recommended fix (b)

Add a middleware that validates `X-Internal-Token` when `OF_INTERNAL_TOKEN` is set:

```go
func internalTokenMiddleware(token string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if token == "" {
                next.ServeHTTP(w, r)  // dev mode: no auth
                return
            }
            if r.Header.Get("X-Internal-Token") != token {
                http.Error(w, "forbidden", http.StatusForbidden)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

Apply it to the router. Configure the gateway to inject the header on its calls.

#### Alternative fix (a) — mTLS

More correct but requires Istio or Linkerd. Overkill for a PoC.

### 6.4 Apache Arrow upgrade

#### Today
`services/pipeline-runner-spark/build.sbt`:
```scala
"org.apache.iceberg" % "iceberg-spark-runtime-3.5_2.12" % "1.5.2" % Provided,
"org.apache.iceberg" % "iceberg-aws-bundle"            % "1.5.2" % Provided,
```

And the workaround:
```
spark.sql.parquet.enableVectorizedReader=false
spark.sql.iceberg.vectorization.enabled=false
```

#### Fix
1. Bump to Iceberg `1.6.x` or `1.7.x` (check compatibility with Spark 3.5.4).
2. Re-build the JAR.
3. Re-build the `pipeline-runner` image and re-deploy.
4. Remove the workaround from `infra/dev/poc-pipeline-nodes.yaml` and from any SparkApplication CR that has it.
5. Smoke test with `infra/dev/spark-smoke.yaml`.

#### Risk
Iceberg APIs can change between minor versions. Use a test branch before merging to main.

### 6.5 Re-scale stopped services

For the dashboard to have lineage, action types, etc., bring up:

```sh
for svc in lineage-service ai-evaluation-service ontology-actions-service notebook-runtime-service; do
  kubectl -n openfoundry scale deploy/$svc --replicas=1
done
```

Watch memory:

```sh
kubectl top nodes
kubectl -n openfoundry top pods --sort-by=memory
```

If it goes over 90% on a node, scale down some less critical service (e.g. `agent-runtime-service`).

---

## 7 · Task 5 — Polish

### 7.1 Formal ADR

Migrate the P4 section of `RUNTIME-INDEXER.md` to a numbered ADR.

#### Steps

```sh
# 1. Find the highest number in use
ls docs/architecture/adr/ | sort -n | tail -5

# 2. Create the file
cp docs/architecture/adr/0001-*.md docs/architecture/adr/NNNN-objectstore-canonical-iceberg-audit.md
```

Minimal template:

```md
# NNNN. Canonical ObjectStore, Iceberg as audit log

Date: 2026-05-10
Status: Accepted

## Context
[transcribe from RUNTIME-INDEXER.md § P4]

## Decision
Cassandra is the canonical source for mutable object state.
Iceberg is the canonical source for the immutable action log.

## Consequences
- (+) Action latency < 100ms (Cassandra LWT).
- (+) Auditability and time-travel without sacrificing hot-path latency.
- (-) Two systems to maintain; consistency is eventual between Kafka → Iceberg.
- (-) A new snapshot-expiry job to operate.
```

### 7.2 Unit tests

Pattern to follow: [ontology-definition-service/internal/handlers/handlers_test.go](../../services/ontology-definition-service/internal/handlers/handlers_test.go).

#### 7.2.1 `application-composition-service/internal/handlers/apps_test.go`

```go
package handlers_test

func TestCreateAppRequiresName(t *testing.T) {
    h := newTestHandlers(t)
    req := httptest.NewRequest("POST", "/api/v1/apps",
        strings.NewReader(`{}`))
    req = withAuthClaims(req, testUserUUID)
    rec := httptest.NewRecorder()
    h.CreateApp(rec, req)
    require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPublishAppCreatesV1(t *testing.T) { ... }
func TestGetPublishedAppByPublicSlug(t *testing.T) { ... }
```

Mock the Repo:
```go
type mockRepo struct{ apps map[uuid.UUID]*models.App }
func (m *mockRepo) GetApp(...) (*models.App, error) { ... }
// ... remaining methods
```

#### 7.2.2 `object-database-service/internal/handlers/objects_bridge_test.go`

Critical cases:
- `toOntologyObject` maps `created_at_ms` → correct RFC3339 UTC.
- Empty markings don't appear in the JSON (`omitempty`).
- `per_page > 5000` is capped at 5000.
- The `x-of-tenant` header overrides the default tenant.
- `properties` is empty when the payload is `null` or malformed.

#### 7.2.3 `ontology-definition-service/internal/handlers/properties_links_test.go`

Cases:
- `ListProperties`: 401 without auth, 400 with a non-uuid type_id.
- `CreateProperty`: 400 without name or property_type, 201 with a valid body.
- `ListLinkTypes` with `?object_type_id=` filter applies the WHERE.
- `CreateLinkType`: 400 if source_type_id or target_type_id are missing.

### 7.3 PoC README update

Append to `docs/poc-online-retail/README.md`:

```md
## Next steps

See [NEXT-STEPS.md](NEXT-STEPS.md) for the detailed roadmap.

The runtime bridge today is implemented in
[`services/object-database-service/internal/handlers/objects_bridge.go`](../../services/object-database-service/internal/handlers/objects_bridge.go).
The real indexer (vs the Python seeder we use today) is described in
[RUNTIME-INDEXER.md](RUNTIME-INDEXER.md).
```

---

## 8 · Command cheatsheet

### 8.1 Build & deploy a Go service

```sh
SVC=application-composition-service
TAG=$(date +%Y%m%d-%H%M%S)

docker buildx build --platform linux/arm64 \
  --build-arg SERVICE_NAME=$SVC \
  --build-arg TARGETOS=linux \
  --build-arg TARGETARCH=arm64 \
  --build-arg VERSION=$TAG \
  -f services/$SVC/Dockerfile \
  -t localhost:30501/$SVC:$TAG --push .

kubectl -n openfoundry set image deploy/$SVC \
  app=registry.registry.svc.cluster.local:5000/$SVC:$TAG
kubectl -n openfoundry rollout status deploy/$SVC --timeout=120s
kubectl -n openfoundry logs deploy/$SVC --tail=20
```

### 8.2 Build the Spark JAR

```sh
cd services/pipeline-runner-spark
sbt clean assembly
ls target/scala-2.12/*.jar
cd ../..

# Re-build the pipeline-runner image, which copies the JAR
docker buildx build --platform linux/arm64 \
  --build-arg VERSION=$(date +%Y%m%d-%H%M%S) \
  -f services/pipeline-runner/Dockerfile \
  -t localhost:30501/pipeline-runner:dev --push .
```

### 8.3 Apply a SparkApplication manually

```sh
kubectl -n openfoundry apply -f infra/dev/spark-smoke.yaml
kubectl -n openfoundry get sparkapplications -w
kubectl -n openfoundry logs <driver-pod-name> -f
```

### 8.4 Debug

```sh
# Service logs
kubectl -n openfoundry logs deploy/<svc> -f --tail=50

# Node memory
kubectl top nodes
kubectl -n openfoundry top pods --sort-by=memory

# Describe a CrashLooping pod
kubectl -n openfoundry describe pod <pod>

# Re-mint JWT (if /tmp/of-jwt expired)
TOKEN=$(curl -s -X POST http://localhost:18080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"smoke@openfoundry.local","password":"openfoundry-smoke-password"}' \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["access_token"])')
echo "$TOKEN" > /tmp/of-jwt
```

### 8.5 Smoke via the gateway

```sh
TOKEN=$(cat /tmp/of-jwt)
TX=678b55fe-db5f-4d3a-bbf2-8cb643af8d32

# Gateway port 18080 (operational)
curl -s "http://localhost:18080/api/v1/ontology/types/$TX/objects?per_page=5" \
  -H "Authorization: Bearer $TOKEN" | python3 -m json.tool

# Gateway port 8080 (needed for SPA dev)
curl -s "http://localhost:8080/api/v1/ontology/types/$TX/objects?per_page=5" \
  -H "Authorization: Bearer $TOKEN" | python3 -m json.tool
```

---

## 9 · Map of relevant files

### Already implemented (reference)

| File | Purpose |
|---|---|
| [services/object-database-service/internal/handlers/objects_bridge.go](../../services/object-database-service/internal/handlers/objects_bridge.go) | Bridge `/api/v1/ontology/types/{id}/objects` (List/Get/Create/**Patch** with merge + LWT) |
| [services/application-composition-service/internal/handlers/apps.go](../../services/application-composition-service/internal/handlers/apps.go) | `/api/v1/apps` CRUD + publish |
| [services/ontology-definition-service/internal/handlers/properties_links.go](../../services/ontology-definition-service/internal/handlers/properties_links.go) | Properties + LinkTypes routes |
| [tools/online-retail/seed_object_database.py](../../tools/online-retail/seed_object_database.py) | PoC seeder (to be replaced by the real indexer) |
| [tools/online-retail/dashboard-app-definition.json](../../tools/online-retail/dashboard-app-definition.json) | App fixture (3 pages, 7 widgets). **WIP unstaged:** `var_anomalies` with direct `static_filter`. |
| [apps/web/src/routes/apps/WorkshopEditorPage.tsx](../../apps/web/src/routes/apps/WorkshopEditorPage.tsx) | **WIP unstaged:** `static_filter` support on variables, page navigation in preview, hook order fix, defensive widget defaults. |

### To create

| File | What for |
|---|---|
| `services/iceberg-indexer-service/...` | New service (Task 1.1) |
| `services/pipeline-runner-spark/src/main/scala/com/openfoundry/indexer/IcebergToObjectStoreIndexer.scala` | Indexer Scala main (Task 1.2) |
| `services/pipeline-runner-spark/src/main/scala/com/openfoundry/audit/ActionLogStreamSink.scala` | Streaming sink Kafka → Iceberg (Task 2) |
| `infra/helm/infra/spark-jobs/templates/_indexer-run-template.yaml` | CR template (Task 1.4) |
| `docs/architecture/adr/NNNN-objectstore-canonical-iceberg-audit.md` | Formal ADR (Task 5.1) |

### To modify

| File | Change |
|---|---|
| [services/object-database-service/internal/storage/types.go](../../services/object-database-service/internal/storage/types.go) | Add `Count()` to the interface (Task 4.1) |
| [services/object-database-service/internal/handlers/objects_bridge.go](../../services/object-database-service/internal/handlers/objects_bridge.go) | Use real `Count()`, expose cursor (4.1, 4.2) |
| [services/object-database-service/internal/server/server.go](../../services/object-database-service/internal/server/server.go) | Internal-token middleware (Task 4.3) |
| `services/ontology-actions-service/internal/handlers/execute.go` | Publish to Kafka after Execute (Task 2.1) |
| `services/pipeline-build-service/internal/...` | Auto-trigger the indexer when a pipeline completes (Task 1.5) |
| [services/pipeline-runner-spark/build.sbt](../../services/pipeline-runner-spark/build.sbt) | Bump Iceberg → 1.6+ (Task 4.4) |
| `services/datasets-service/internal/repo/migrations/...` | Add `target_object_type_id`, `target_id_column` (Task 1.5) |

---

## 10 · Recommended execution order

1. **Close Task 0 (Browser pass)** — the patches exist unstaged. Validate
   the "Mark resolved" round-trip and commit. **30 min.**
2. **Server-side filter pushdown** (discovered during the browser pass) —
   expose `POST /objects/query` on the bridge with a filter-spec. Without
   this, anything beyond 10⁴ rows in the dashboard breaks the client.
   **3 h.** *(was not in the initial version of the doc; added by the
   browser pass)*
3. **Tasks 4.1 + 4.2 (Count + cursors)** — aligns with #2 (same handler). **2 h.**
4. **Task 3 (Cassandra)** — without this, data is lost on restart. **3 h.**
5. **Task 1 (Real indexer)** — the production piece. **2-3 days.**
6. **Task 2 (Audit log)** — unblocks time-travel over actions. **1-2 days.**
7. **Tasks 4.3 + 4.4 (auth + Arrow upgrade)** — hardening. **1 day.**
8. **Task 5 (Polish: ADR + tests + README)** — at the end. **1 day.**

Total estimate: **6-8 person-days** from the current state to a complete, end-to-end, stub-free PoC.

---

## 11 · If you get stuck

- **Spark driver logs:** `kubectl -n openfoundry logs <driver-pod> -f`. Apache Arrow / Iceberg usually leave clear messages.
- **The gateway isn't routing what you expect:** check
  [services/edge-gateway-service/internal/proxy/router_table.go](../../services/edge-gateway-service/internal/proxy/router_table.go).
  It's a big switch but readable.
- **The SPA is calling an endpoint that doesn't exist:** `grep -rn '/api/v1/<path>' apps/web/src/lib/api/` and compare with
  [router_table.go](../../services/edge-gateway-service/internal/proxy/router_table.go)
  to see which backend it routes to.
- **CNPG Postgres cluster isn't responding:**
  ```sh
  kubectl -n openfoundry get clusters.postgresql.cnpg.io
  kubectl -n openfoundry logs pg-runtime-config-1 --tail=50
  ```
- **Lima OOM:** `limactl shell k3s-master -- free -m`. If it's red, scale services down or grow the VM memory (requires stop+start).
- **Something was forgotten:** commit `fa60eef9` is the baseline. `git diff fa60eef9 HEAD -- '*.go'` to see what's been added since. The agent's persistent memory lives in
  `~/.claude/projects/-Users-torrefacto-Documents-Repositorios-OpenFoundry/memory/` —
  it contains context on Lima, parallel agents, and this PoC.
