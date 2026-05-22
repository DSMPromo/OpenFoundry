<div align="center">
  <a href="https://github.com/DSMPromo/OpenFoundry">
    <img src="images/logo.png" alt="OpenFoundry" width="420" />
  </a>


<p align="center">The Open-Source Data Operating System</p>

<p align="center">
  <a href="https://github.com/DSMPromo/OpenFoundry/actions/workflows/openfoundry-go.yml"><img alt="Go CI" src="https://github.com/DSMPromo/OpenFoundry/actions/workflows/openfoundry-go.yml/badge.svg" /></a>
  <a href="https://github.com/DSMPromo/OpenFoundry/actions/workflows/ci-frontend.yml"><img alt="Frontend CI" src="https://github.com/DSMPromo/OpenFoundry/actions/workflows/ci-frontend.yml/badge.svg" /></a>
  <a href="https://github.com/DSMPromo/OpenFoundry/actions/workflows/proto-check.yml"><img alt="Proto Check" src="https://github.com/DSMPromo/OpenFoundry/actions/workflows/proto-check.yml/badge.svg" /></a>
  <img alt="GitHub commit activity" src="https://img.shields.io/github/commit-activity/m/DSMPromo/OpenFoundry" />
  <img alt="GitHub Last Commit" src="https://img.shields.io/github/last-commit/DSMPromo/OpenFoundry" />
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/License-AGPL--3.0--only-blue.svg" /></a>
</p>

<p align="center">
  <a href="docs/">Getting Started</a>
  · <a href="docs/">Docs</a>
  · <a href="https://github.com/DSMPromo/OpenFoundry/issues">Bug reports</a>
  · <a href="https://github.com/DSMPromo/OpenFoundry/discussions">Discussions</a>
</p>

OpenFoundry is an open-source operational data platform inspired by the capability model of Palantir Foundry, implemented as auditable, extensible software. It combines **51 service directories**, **38 shared libraries**, Protobuf/OpenAPI contracts, generated SDKs, a **React 19 + Vite + TypeScript** web console, and declarative infrastructure for Kubernetes.

Unlike closed data platforms, OpenFoundry ships as a single Go monorepo with stable contracts, GitOps-native delivery, and governance that lives next to the code. The goal is to provide a reproducible foundation for teams that need to connect sources, version datasets, model an ontology, expose APIs, automate workflows, govern access, and operate analytical or AI workloads with end-to-end traceability.

> **Working with this codebase as an AI agent?** Start at [`CLAUDE.md`](CLAUDE.md). It is the canonical onboarding guide for commands, conventions, security-critical zones, and what not to read by default.

## Feature & Status

- **Cloud-Native Architecture**: Small Go services with one entrypoint per service, delivered through Helm, ArgoCD, and Terraform.
- **Ontology at the Core**: Object types, actions, functions, object views, lineage, and stable contracts for building applications on operational data.
- **Contracts First**: Protobuf as the source of truth, generated OpenAPI, and synchronized TypeScript, Python, and Java SDKs.
- **Integrated Governance**: Authentication, authorization, Cedar policies, audit, tenancy, SSO/MFA, and egress controls.
- **Observability by Default**: `/healthz`, `/metrics`, Prometheus, Grafana, Mimir, structured logs, and OTel traces.
- **Developer Platform**: CLI tooling, SDK generation, service templates, VitePress docs, and unit/integration test paths.
- **Single Go Module**: One root `go.mod` keeps `libs/` and `services/` synchronized without version drift.

| Capability                     | Status                       | Capability                  | Status                       |
| :----------------------------- | :--------------------------- | :-------------------------- | :--------------------------- |
| **Datasets & Versioning**      | ✅ Available                 | **Ontology Services**       | ✅ Available                 |
| **React Web Console**          | ✅ Available                 | **Generated SDKs**          | ✅ Available                 |
| **Protobuf/OpenAPI Contracts** | ✅ Available                 | **AuthN/AuthZ Foundations** | ✅ Available                 |
| **Observability Stack**        | ✅ Available                 | **Helm/ArgoCD Delivery**    | ✅ Available                 |
| **Kafka/NATS Integrations**    | ✅ Available                 | **Lakehouse / Iceberg**     | 🚧 Under active development  |
| **AI / Agent Runtime**         | 🚧 Under active development  | **Production Hardening**    | 🚧 In progress               |

## OpenFoundry vs Closed Data Platforms

| Feature              | OpenFoundry                                                                                                                       | Closed Data Platforms                                                                  |
| :------------------- | :-------------------------------------------------------------------------------------------------------------------------------- | :------------------------------------------------------------------------------------- |
| **Control**          | **Auditable Code & Contracts**<br>One monorepo with services, libraries, SDKs, and infra fully inspectable.                       | **Vendor Lock-in**<br>Strong provider dependency and limited implementation visibility. |
| **Extensibility**    | **Open Ecosystem**<br>Services, libraries, SDKs, and docs evolve with your needs.                                                 | **Bounded by APIs**<br>Limited by external APIs and vendor roadmaps.                   |
| **Deployment**       | **GitOps-Native**<br>Kubernetes, Helm, ArgoCD, Terraform, and Compose for reproducible environments.                              | **SaaS-First**<br>Managed deployments with less operational control.                   |
| **Governance**       | **In-Tree Policies & Audit**<br>Policies, audit, and tenancy live beside the platform code.                                       | **Coupled to Product**<br>Governance is tied to the vendor's commercial boundaries.    |
| **Developer Flow**   | **Standard Tooling**<br>Go, TypeScript, Python, Java, Protobuf, and Makefile workflows.                                           | **Proprietary Tooling**<br>Local workflows that are harder to automate.                |
| **Contracts**        | **Wire-Compatibility Invariants**<br>Pinned by golden tests; RIDs, JWT claims, and resource registries are stable.                | **Opaque Contracts**<br>Internal APIs change without notice.                           |
| **Licensing**        | **AGPL-3.0-only**<br>Strong copyleft, transparent governance, no hidden terms.                                                    | **Closed Source**<br>Opaque pricing and IP boundaries.                                 |

## Staying ahead

Star OpenFoundry on GitHub and be instantly notified of new releases.

## Quickstart

To get started with OpenFoundry, follow these steps:

### 1. Clone the repository

```bash
git clone https://github.com/DSMPromo/OpenFoundry.git
cd OpenFoundry
```

### 2. Local development (Option 1)

Install the Go tools used by the monorepo into `./bin`, including `buf`, `golangci-lint`, `sqlc`, and `gofumpt`:

```bash
make tools
```

Run the canonical local gate (tidy + vet + lint + contract checks + unit tests):

```bash
make ci
```

For faster iteration:

```bash
make test              # unit tests with -race + coverage
make build             # compile all packages
make contracts-check   # verify OpenAPI + SDK drift
```

### 3. Frontend (Option 2)

The web console lives in [`apps/web/`](apps/web/) and uses React 19, Vite, and TypeScript:

```bash
pnpm install
pnpm --filter @open-foundry/web dev      # vite dev server
pnpm --filter @open-foundry/web check    # tsc -b --noEmit
pnpm --filter @open-foundry/web test     # vitest
```

### 4. Docker Compose (Option 3)

Bring up local infrastructure (Postgres, Kafka, NATS, observability stack, …):

```bash
docker compose -f infra/compose/docker-compose.yml up -d
```

For development with auto-reload services:

```bash
docker compose -f infra/compose/docker-compose.dev.yml up -d
```

### 5. Helm / Kubernetes (Option 4) - Cloud Native

Delivery assets live in [`infra/`](infra/): Helm charts, ArgoCD apps, Terraform, Compose, and operational runbooks.

```bash
make gitops-bootstrap     # bootstrap ArgoCD applications
make gitops-status        # check sync status
make build-services       # one binary per service into ./bin/
```

### 6. Code generation (Option 5)

Regenerate Protobuf Go, sqlc, OpenAPI, and SDKs whenever contracts change:

```bash
make gen
make contracts-check
```

> **Heads-up**: `make lint` baselines pre-existing issues via `.golangci.yml` (`new-from-rev: HEAD`). To audit the full backlog, run `golangci-lint run --new-from-rev= ./...`.

---

### Accessing OpenFoundry

1. **Web Console**: Open `http://localhost:5173` (Vite dev server) or the URL exposed by your Kubernetes ingress.
2. **APIs**: Each service exposes `/healthz`, `/metrics`, and a versioned `/api/v1/...` surface.
3. **CLI**: Use `of-cli` from [`tools/of-cli/`](tools/of-cli/) for service introspection, contract checks, and route audits.

## Repository layout

```text
OpenFoundry/
├── apps/web/         React 19 + Vite + TypeScript frontend
├── services/         51 Go microservices (copy docs/templates/service-skeleton/ for new ones)
├── libs/             38 shared Go packages (auth, observability, kernels, …)
├── proto/            Protobuf source of truth; Go generated into libs/proto-gen/
├── sdks/             Generated TypeScript, Python and Java SDKs
├── infra/            Helm, ArgoCD, Terraform, Compose and operational runbooks
├── docs/             VitePress capability-oriented documentation site
├── tools/            CLIs and lint/helper tools
├── images/           Project branding assets, including this README logo
├── go.mod            Single Go module for the entire monorepo
└── Makefile          Canonical local task runner
```

## Documentation

For detailed documentation, including configuration options, API references, ADRs, and advanced usage, please visit:

- [`docs/`](docs/) — capability-oriented technical documentation.
- [`ARCHITECTURE.md`](ARCHITECTURE.md) — high-level architecture overview.
- [`docs/architecture/adr/`](docs/architecture/adr/) — dated architectural decisions.
- [`CLAUDE.md`](CLAUDE.md) — concise onboarding for AI agents.
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — PR process, RFC requirements, and DCO policy.
- [`SECURITY.md`](SECURITY.md) — how to report vulnerabilities.

## Getting Help

If you have any questions or need assistance:

- Check the existing [GitHub Discussions](https://github.com/DSMPromo/OpenFoundry/discussions) for common issues and solutions.
- Ask questions and share your experiences in [Discussions](https://github.com/DSMPromo/OpenFoundry/discussions).
- Open an issue on [GitHub Issues](https://github.com/DSMPromo/OpenFoundry/issues) for bug reports or feature requests.

## Links

- [Documentation](docs/) — The manual you should read
- [Changelog](https://github.com/DSMPromo/OpenFoundry/releases) — Releases and notable changes
- [GitHub Discussions](https://github.com/DSMPromo/OpenFoundry/discussions) — Where the community lives

## Contact

- **Bugs**: [GitHub Issues](https://github.com/DSMPromo/OpenFoundry/issues)
- **General Discussion**: [GitHub Discussions](https://github.com/DSMPromo/OpenFoundry/discussions)
- **Security**: [SECURITY.md](SECURITY.md)
- **Contributing**: [CONTRIBUTING.md](CONTRIBUTING.md)

## Contributors

OpenFoundry is a community-driven project, and we appreciate all contributions. Check out the [Contributors](https://github.com/DSMPromo/OpenFoundry/graphs/contributors) page to see the amazing people who have helped make OpenFoundry better.

<a href="https://github.com/DSMPromo/OpenFoundry/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=DSMPromo/OpenFoundry" alt="Contributors" />
</a>

## Star History


<a href="https://www.star-history.com/?repos=DSMPromo%2FOpenFoundry&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=DSMPromo/OpenFoundry&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=DSMPromo/OpenFoundry&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=DSMPromo/OpenFoundry&type=date&legend=top-left" />
 </picture>
</a>

## License

[AGPL-3.0-only](https://opensource.org/licenses/AGPL-3.0)

**OpenFoundry** is a trademark of the OpenFoundry project. All other trademarks are the property of their respective owners.

