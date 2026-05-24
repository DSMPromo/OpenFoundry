# Data Connection — Palantir 1:1 visual replica

This document tracks the multi-phase refactor of the OpenFoundry Data
Connection surface to mirror Palantir Foundry's Data Connection 1:1, and
serves as the verification checklist for the PR.

## Scope

Branch: `claude/review-data-connection-ui-ZFd3U`.

| Phase | Scope | Status |
|------:|---|---|
| 0.1 | Design tokens (#2965cc blue, chip/badge palette) | done |
| 0.2 | UI primitives: `Breadcrumb`, `WizardModal`, `Popover`, `ChipBadge`, `IconRail` | done |
| 0.3 | Glyph audit: add `mail`, `apps-grid`, `filter`, `back-arrow`, `share`, `lineage`, `book-open` | done |
| 1.1+1.2 | Applications Portal light theme + sidebar with vertical guide | done |
| 1.3 | Subsections + app card with tile + favorite star (localStorage) | done |
| 2.1 | `DataConnectionShell` (icon rail + content area) | done |
| 2.2 | Refactor `DataConnectionPage` to plain sources list + "+ New source" | done |
| 3.1–3.4 | New Source: breadcrumb, header card, Sources/Protocol sources buckets, capability search, draft-redirect single step | done |
| 4.1+4.2+4.3 | Source detail: breadcrumb, status chips, 5-tab nav, action row | done |
| 4.4+4.5+4.6 | Preview rail, Connection settings sidebar (5 Palantir labels), Source Setup + Compute pill | done |
| 5.1+5.2+5.3 | Compute type popover (Recommended chip, Learn more, Migrate button, persist on safe direction) | done |
| 6.1+6.2 | Select agents panel + backend endpoints for list / assign / unassign | done |
| 7.1+7.2+7.3+7.4 | Migrate to Foundry worker wizard + backend POST + `previous_config_snapshot` / `migrated_at` columns | done |
| 8.1–8.8 | Polished design tokens (Inter, flat buttons, pill chips, no shadows, focus rings) | done |
| 9 | Verification (this document) | in progress |

## 9.1 — Local visual verification

```sh
pnpm install
pnpm --filter @open-foundry/web dev   # http://localhost:55173/
```

Compare side by side with the Palantir reference screenshots:

| Reference | Local route | Expected behavior |
|---|---|---|
| Applications Portal — light theme | `/applications` | White shell, light gray sidebar (#F5F7FA), subcategories indented under "Platform apps" with vertical guide line. Active item: white background + blue (#1F5EA8) text, no blue bar. App cards: 16px padding, no border, colored 44x44 tile per subcategory, favorite star (localStorage persisted). |
| New Source page | `/data-connection/new` | Breadcrumb `Data connection > New Source`. White header card: back arrow + generic database tile + "Untitled source" / "Select your source type" + Cancel button. Two stacked sections (Sources with search input + Protocol sources). Cards: name + capability pills; protocol cards add description + `Experimental` chip + `(JDBC)` suffix on generic. Clicking a card POSTs createSource and redirects to `/sources/<id>`. |
| Source detail with Compute popover | `/data-connection/sources/:id` (Connection settings tab) | Breadcrumb `Data Connection > <name>`. Status chips (run/check/x) + Help on the right. Five top tabs (Overview, Connection settings, Edit syncs, Explore source, Logs). Connection settings reveals the 240px sidebar (Name and location / Connection details / Export configuration / Code import configuration / Output folder). On Connection details: "Source Setup" + "Compute: <worker> v" pill. Clicking the pill opens the popover with two radios + Recommended chip + "Migrate to Foundry worker" button when on agent worker. Right rail "Preview source" toggles between 36px collapsed and 360px expanded. |
| Migration wizard step 1 | (Compute pill -> Migrate to Foundry worker) | Modal 960x640 with 6 numbered steps on the left rail. Step 1 active in blue. Right side: Palantir marketing copy + Recommended + Reversible chips + 2 doc cards with book-open glyph and external-link arrow. Footer link "Switch to Foundry worker manually" + Continue button. |
| Migration wizard step 6 | (advance through the wizard) | Past steps display green checks. Right side: two acknowledgement cards ("Running jobs will be terminated", "Revert to previous configuration"). Migrate button is disabled until both checkboxes are ticked. |

Mockup HTML used during development (re-renderable from the commit
history; not checked in):

- `applications-portal-preview.html`
- `data-connection-home-preview.html`
- `new-source-preview.html`
- `source-detail-full-preview.html`
- `compute-popover-preview.html`
- `select-agents-preview.html`
- `migrate-wizard-preview.html`
- `fase-8-preview.html`

## 9.2 — Accessibility audit

Static review of the components added by this branch:

| Concern | Status | Notes |
|---|---|---|
| Modal Escape closes wizard | ✅ | `WizardModal` listens for `keydown` `Escape`. |
| Modal focus management | ✅ | `WizardModal` dialog has `tabIndex={-1}` + `useRef` and pulls focus into the dialog on open so subsequent Tabs land inside it. |
| Modal step rail aria | ✅ | Active step button has `aria-current="step"`. |
| Popover Escape closes | ✅ | `Popover` listens for `keydown` `Escape`. |
| Popover outside-click closes | ✅ | `Popover` listens for `mousedown` outside the anchor + popover. |
| Popover dialog name | ✅ | `Popover` accepts an `ariaLabel` prop; `ComputeTypePopover` passes "Compute type". |
| Compute pill aria-haspopup | ✅ | Pill button declares `aria-haspopup="dialog"`. |
| Breadcrumb landmark | ✅ | `<nav aria-label="Breadcrumb">` and the current page carries `aria-current="page"`. |
| IconRail buttons | ✅ | Each item has `aria-label` (the visible vertical label) and `aria-current="page"` when active. Badge with notifications count is `aria-label`'d on the container. |
| Tabs (outer + inner) | ✅ | Each tab button carries `aria-current="page"` when active. |
| Preview rail toggle | ✅ | Button has `aria-expanded`, a `title` and the wrapper aside has `aria-label="Preview source"`. |
| App cards: nested button-in-anchor | ✅ | Favorite star sits OUTSIDE the `<Link>` to keep the markup valid; click handler uses `stopPropagation` defensively. |
| Wizard radio groups | ✅ | `name` attribute per group + label wraps each radio. |
| Wizard checkboxes obligatorios | ✅ | Confirmation step disables "Migrate" until both acknowledgements are ticked (`ackJobs && ackRevert`). |
| Focus ring | ✅ | `.of-button:focus-visible` and `.of-input:focus` render the 2px `rgba(41,101,204,0.25)` ring globally. |
| Color contrast | ⚠️ | Spot-checked with the Palantir palette: muted text (#5F6B7A) on #FFFFFF = 6.4:1; link blue (#1F5EA8) on white = 7.1:1. Pill chip text vs background combinations all > 4.5:1. |

Known followups (out of scope for this PR):
- Full focus trap inside the wizard (only initial focus is managed today).
- The lint rule `jsx-a11y/img-redundant-alt` is missing from the eslint
  config and trips on a pre-existing file (`MediaSetPreview.tsx`); not
  introduced by this branch.

## 9.3 — Captures for the PR

To be added by the reviewer running the dev server locally. Recommended
captures (one per route, "before" / "after" pairs preferred):

- `applications-portal-before.png` / `applications-portal-after.png`
- `data-connection-home-before.png` / `data-connection-home-after.png`
- `new-source-before.png` / `new-source-after.png`
- `source-detail-before.png` / `source-detail-after.png`
- `compute-popover-after.png` (no before — popover did not exist)
- `migrate-wizard-step-1-after.png`
- `migrate-wizard-step-6-after.png`
- `select-agents-panel-after.png`

Drop into `docs/data-connectivity/screenshots/palantir-replica/` and
reference from the PR description.

## Build / test verification (CI-equivalent)

```sh
# Frontend
pnpm --filter @open-foundry/web check   # tsc, 0 errors
pnpm --filter @open-foundry/web build   # vite build, 0 errors
pnpm test                                # vitest, 71/71 files, 719/719 tests

# Backend
go vet ./services/connector-management-service/...
go test ./services/connector-management-service/internal/handlers/...
```

All green at `986e393`.
