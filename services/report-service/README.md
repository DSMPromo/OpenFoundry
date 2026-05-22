# report-service

Operational reporting backend for OpenFoundry. It owns report
definitions, runs report executions, and renders downloadable artifacts
(PDF, Excel, CSV, HTML, PowerPoint). The edge gateway routes
`/api/v1/reports*` here via the `u.Report` upstream
(`services/edge-gateway-service/internal/proxy/router_table.go`).

## Exposed surface

Every `/api/v1/reports` route requires a valid JWT.

| Method | Path | Purpose |
|---|---|---|
| GET   | `/healthz` | liveness payload |
| GET   | `/metrics` | Prometheus scrape endpoint |
| GET   | `/api/v1/reports/overview` | report counts + latest execution |
| GET   | `/api/v1/reports/catalog` | available generators + delivery channels |
| GET   | `/api/v1/reports/definitions` | list report definitions |
| POST  | `/api/v1/reports/definitions` | create a report definition |
| PATCH | `/api/v1/reports/definitions/{id}` | patch a report definition |
| POST  | `/api/v1/reports/definitions/{id}/generate` | run the report, store an execution |
| GET   | `/api/v1/reports/definitions/{id}/history` | executions for one report |
| GET   | `/api/v1/reports/schedules` | schedule board |
| GET   | `/api/v1/reports/executions/{id}` | one execution record |
| GET   | `/api/v1/reports/executions/{id}/download` | execution + artifact metadata (JSON) |
| GET   | `/api/v1/reports/executions/{id}/artifact` | the rendered report file (binary) |

## Persistence

- `internal/repo` is a pgx-backed store over Postgres (`report_definitions`,
  `report_executions`); embedded SQL migrations run on boot.
- With no `DATABASE_URL` / `OF_DATABASE__URL`, an in-memory store is used
  when `OF_REPORT_ALLOW_MEMORY_STORE=true` or `OPENFOUNDRY_ENV` is
  `dev`/`test`/`local`. In production the service fails closed.

## Artifact generation

`internal/generator` renders an execution into a downloadable file using
the Go standard library only — no third-party dependencies:

- **CSV** / **HTML** — `encoding/csv` and `html/template`.
- **PDF** — hand-written PDF 1.7 (Helvetica standard font, nothing embedded).
- **Excel** (`.xlsx`) / **PowerPoint** (`.pptx`) — OOXML packages assembled
  with `archive/zip`.

Every renderer is a pure, deterministic function of the execution, so the
checksum recorded by `generate` matches the bytes streamed by `/artifact`
without the blob ever being persisted.

## Build

```sh
go build -o bin/report-service ./services/report-service/cmd/report-service
```
