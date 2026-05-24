# Hoja de Ruta — Del 5% al 100% de Cobertura E2E (Playwright)

> **Estado actual:** ~33 casos `test()` en `apps/web/e2e/` contra ~135 páginas (`*Page.tsx`) y ~141 rutas → **~5% de cobertura funcional**.
>
> **Objetivo:** Cobertura E2E del 100% de pantallas, botones y flujos funcionales del frontend de OpenFoundry, ejecutada en CI en 12 shards (3 browsers × 4 shards), con cross-cutting de accesibilidad, regresión visual y rendimiento.
>
> **Cómo usar este documento:** Cada tarea es un **prompt autocontenido para Claude Code**. Pega el bloque del título de la tarea + el cuerpo en una nueva sesión de Claude Code (con el repo `openfoundry-go` montado) y será suficiente para completarla. Las tareas están ordenadas por dependencia. **Las Fases 0 y 1 son prerrequisitos** del resto.
>
> **Convenciones de referencia (no repetidas en cada prompt):**
> - Repo: `/home/user/OpenFoundry`. Frontend: `apps/web/`. Tests E2E: `apps/web/e2e/`.
> - Config Playwright: `apps/web/playwright.config.ts` (baseURL `http://127.0.0.1:55173`).
> - Patrones existentes a imitar: `apps/web/e2e/workshop-actions.spec.ts`, `apps/web/e2e/route-smokes.spec.ts`, `apps/web/e2e/auth-smoke.spec.ts`, `apps/web/e2e/smoke-navigation.spec.ts`.
> - Fixtures: `apps/web/e2e/fixtures/base.ts` (test extendido con `authedPage`), `mocks.ts` (`mockAuth`, `mockJson`, `buildUser`), `workshop.ts` (`defineWorkshopApp`, `mockWorkshopApp`).
> - Page Objects existentes: `LoginPage`, `AppShellPage`, `WorkshopRuntimePage`, `ProjectsPage`, `SettingsPage` en `apps/web/e2e/pages/`.
> - Helpers: `apps/web/e2e/helpers/a11y.ts` (`expectNoA11yViolations`).
> - Selectores: priorizar `getByRole`, `getByLabel`, `getByText`, `getByTestId`. CSS classes solo como fallback.
> - Timestamp determinista: `const E2E_NOW = '2026-05-11T00:00:00Z'`.
> - Wire types: envelopes `models.Page[T]` y `models.ListResponse[T]` (Go). `GET /api/v1/...` lista devuelve `{ data: [], next_cursor: null, total: 0 }`.
> - CI: `.github/workflows/ci-frontend.yml` (matrix browser × shard, ya configurado).
> - **Reglas de oro:** (a) Mockear `mockAuth` ANTES de `page.goto`. (b) Capturar `pageerror` + `console.error` y filtrar `ERR_ABORTED`/`Failed to load resource`. (c) `expect.poll()` para requests async. (d) `toMatchObject` para validación parcial de payload.

---

## Tabla de contenidos

- [Fase 0 — Infraestructura de testing (prerrequisito)](#fase-0--infraestructura-de-testing-prerrequisito)
- [Fase 1 — Auth completo](#fase-1--auth-completo)
- [Fase 2 — Workspace core (Home, Search, Notifications, Favorites, Recent)](#fase-2--workspace-core)
- [Fase 3 — Projects](#fase-3--projects)
- [Fase 4 — Datasets](#fase-4--datasets)
- [Fase 5 — Tablas y objetos (Virtual, Iceberg, Object*)](#fase-5--tablas-y-objetos)
- [Fase 6 — Pipelines, Schedules, Builds, Foundry Rules](#fase-6--pipelines-schedules-builds-foundry-rules)
- [Fase 7 — Ontology completa](#fase-7--ontology-completa)
- [Fase 8 — Apps y Workshop (cobertura ampliada)](#fase-8--apps-y-workshop-cobertura-ampliada)
- [Fase 9 — Data Connection](#fase-9--data-connection)
- [Fase 10 — AI (Assist, Analyst, Threads, Documents, ChatBot Studio, Operator)](#fase-10--ai)
- [Fase 11 — Functions y Action Types](#fase-11--functions-y-action-types)
- [Fase 12 — Lineage y Visualización (Quiver, Geospatial, Contour, Vertex)](#fase-12--lineage-y-visualizacion)
- [Fase 13 — Notebooks, Notepad, Slate](#fase-13--notebooks-notepad-slate)
- [Fase 14 — Streaming y Media Sets](#fase-14--streaming-y-media-sets)
- [Fase 15 — Model Catalog y AIP Evals](#fase-15--model-catalog-y-aip-evals)
- [Fase 16 — Funcionalidad avanzada](#fase-16--funcionalidad-avanzada)
- [Fase 17 — Settings, Audit/Approvals, Retention](#fase-17--settings-auditapprovals-retention)
- [Fase 18 — Control Panel (17 sub-pantallas)](#fase-18--control-panel)
- [Fase 19 — Apps, OSDK, Custom Endpoints, Widgets, Developers, Pilot, Ontology Indexing](#fase-19--apps-osdk-custom-endpoints-widgets-developers-pilot-ontology-indexing)
- [Fase 20 — Demos, 404 y redirects legacy](#fase-20--demos-404-y-redirects-legacy)
- [Fase 21 — Cross-cutting (a11y, visual, responsive, perf, permisos)](#fase-21--cross-cutting)
- [Fase 22 — CI hardening y dashboard de cobertura](#fase-22--ci-hardening-y-dashboard-de-cobertura)

---

# Fase 0 — Infraestructura de testing (prerrequisito)

> Esta fase debe completarse PRIMERO. Sin ella, todas las fases siguientes duplicarán código.

## Tarea 0.1 — Library completa de Page Objects

```
Eres Claude Code trabajando en el repo OpenFoundry (/home/user/OpenFoundry). El frontend está en apps/web/ (React 19 + Vite + TypeScript). Los tests E2E viven en apps/web/e2e/ y siguen los patrones de apps/web/e2e/pages/LoginPage.ts y AppShellPage.ts.

OBJETIVO: Generar una librería completa de Page Objects, uno por cada área de rutas. Cada Page Object debe exponer:
- Constructor que recibe `page: Page` de Playwright.
- Método `goto()` con la URL canónica del área.
- Locators tipados como propiedades públicas para todos los elementos interactivos visibles en la página (botones primarios, inputs, tablas, tabs, modales raíz).
- Método `expectLoaded()` que aserta que la página se ha renderizado completamente (esperar a un elemento estable, no a "networkidle").
- Métodos de acción de alto nivel (`createX()`, `searchFor(q)`, `openRow(id)`, etc.) para encapsular flujos repetidos.

ÁREAS A CUBRIR (un archivo por área en apps/web/e2e/pages/):
- HomePage, SearchPage, NotificationsPage, FavoritesPage, RecentPage
- ProjectsListPage, ProjectDetailPage, ProjectFolderPage
- DatasetsListPage, DatasetUploadPage, DatasetDetailPage, DatasetBranchesPage, DatasetBranchDetailPage
- VirtualTablesPage, VirtualTableDetailPage, IcebergTablesPage, IcebergTableDetailPage
- ObjectDatabasesPage, ObjectExplorerPage, ObjectViewsPage, ObjectMonitorsPage, ObjectLinkTypesPage
- PipelinesPage, PipelineNewPage, PipelineEditPage, LinterPage, FoundryRulesPage
- NewSchedulePage, ScheduleDetailPage, BuildSchedulesPage, SweepPage, BuildsPage, BuildDetailPage
- OntologyManagerPage, BindingsWizardPage, OntologyHomePage, CreateObjectTypePage, OntologyGraphPage, ObjectSetsPage, ObjectTypeDetailPage
- AppsPage, WorkshopEditorPage, AppRuntimePage
- DataConnectionPage, AgentsPage, EgressPoliciesPage, NewSourcePage, NewStreamingSourcePage, SourceDetailPage
- AiPage, AssistPage, AnalystPage, ThreadsPage, DocumentsPage, ChatbotStudioPage, OperatorPage
- FunctionsPage, ActionTypesPage, ActionTypeDetailPage
- LineagePage, QuiverPage, GeospatialPage, ContourPage, VertexPage
- SlatePage, NotebooksListPage, NotebookDetailPage, NotepadListPage, NotepadDetailPage
- StreamingPage, StreamingDetailPage, MediaSetsPage, MediaSetDetailPage
- MlPage, AipEvalsPage
- MachineryPage, FusionPage, LogicAuthoringPage, DynamicSchedulingPage, InterfacesPage, CodeReposPage, InvestigatorPage, PeerManagerPage, CipherPage, SensitiveDataScannerPage, RetentionPoliciesPage
- AuditPage, ApplicationsPage, OsdkAppsPage, CustomEndpointsPage, CheckpointsPage, CustomWidgetsPage, DevelopersPage, PilotPage, OntologyIndexingPage
- ControlPanelPage + una clase por cada sub-página (Users, Groups, Identity Providers, Tenancy, Role Sets, Marking Categories, Scoped Sessions, Application Access, Third Party Applications, Member Discovery, File Access Presets, Retention Policies, Streaming Profiles, Data Health, Projects, Restricted Views)

PASOS:
1. Lee los Page Objects existentes en apps/web/e2e/pages/ para entender la convención exacta.
2. Para cada área, lee el archivo de la página correspondiente en apps/web/src/routes/<area>/ y extrae los selectores principales (busca `data-testid`, `aria-label`, `role`, textos de botón fijos).
3. Crea el archivo del Page Object. Si la página renderiza un layout estándar (header + tabs + tabla), usa una superclass `ListPagePO` que pondrás en apps/web/e2e/pages/_base.ts.
4. Exporta todo desde apps/web/e2e/pages/index.ts.
5. NO crees specs todavía — solo Page Objects.

CRITERIOS DE ACEPTACIÓN:
- `pnpm --filter @open-foundry/web check` pasa sin errores de TypeScript.
- Cada Page Object tiene al menos `goto()` y `expectLoaded()`.
- Selectores priorizan accesibilidad (getByRole, getByLabel) por encima de CSS classes.

Cuando termines, lista los archivos creados y un resumen de Page Objects que comparten patrones (ej. "12 list pages extienden ListPagePO").
```

## Tarea 0.2 — Factory de mocks de API

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/e2e/fixtures/mocks.ts y workshop.ts para entender el patrón de mocks actual.

OBJETIVO: Construir una factory de mocks de API que cubra TODOS los endpoints REST consumidos por apps/web/. La factory vivirá en apps/web/e2e/fixtures/api-mocks.ts y se invocará desde cada spec.

PASOS:
1. Inventaria endpoints: corre `grep -rE "fetch|axios|/api/v1" apps/web/src/lib/api/` y lista los paths únicos. Espera ~150-300 endpoints distintos en ~30 archivos cliente.
2. Para cada recurso (`datasets`, `projects`, `pipelines`, `ontology/types`, `actions`, `users`, `groups`, `connectors`, etc.) define dos cosas:
   - `make<Resource>(overrides?: Partial<X>): X` — builder de una entidad con valores por defecto deterministas (usa `E2E_NOW = '2026-05-11T00:00:00Z'`).
   - `mock<Resource>List(page: Page, items?: X[], opts?: { total?, error? }): Promise<void>` — instala `page.route` que devuelve `{ data: items, next_cursor: null, total: items.length }`.
   - `mock<Resource>Detail(page: Page, item: X)` — instala `page.route('**/api/v1/<resource>/<id>', ...)`.
3. Añade un helper global `installDefaultApiMocks(page)` que mockee todas las listas como vacías y todos los `POST/PUT/DELETE/PATCH` con 204. Será la red de seguridad para evitar requests reales en tests que no se preocupan por un endpoint específico.
4. Añade helper `captureRequests(page, pattern): { calls: any[] }` que captura todos los bodies de requests que matcheen el pattern. Útil para asserts post-acción.
5. Exporta todo desde apps/web/e2e/fixtures/api-mocks.ts. Documenta con TSDoc cada builder.

CRITERIOS:
- `pnpm --filter @open-foundry/web check` pasa.
- Existe al menos un builder + mockList + mockDetail para cada uno de: datasets, projects, pipelines, schedules, builds, ontology-types, object-types, object-views, actions, action-types, functions, connectors, agents, users, groups, role-sets, marking-categories, identity-providers, notebooks, notepad, media-sets, streaming, ml-models, threads, documents.
- Los IDs por defecto siguen patrón `<recurso>-1`, `<recurso>-2` para previsibilidad.

Reporta: lista de recursos cubiertos y endpoints faltantes (si los hay).
```

## Tarea 0.3 — Fixtures globales de Playwright extendidas

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/e2e/fixtures/base.ts para entender cómo se extiende `test`.

OBJETIVO: Ampliar las fixtures globales para que cada test reciba automáticamente:
- `authedPage` (ya existe).
- `apiMocks`: instancia del factory de la Tarea 0.2 con `installDefaultApiMocks(page)` ya ejecutado.
- `pageErrors`: array vivo de errores `pageerror` + `console.error` capturados. Al final del test, si quedan errores no allowlisted, fail automático.
- `adminPage`: page autenticado como user con `roles: ['admin']`, `permissions: ['*']` (override de buildUser).
- `viewerPage`: page autenticado como user con `roles: ['viewer']`, `permissions: ['read:*']`. Útil para tests de permisos.

PASOS:
1. Edita apps/web/e2e/fixtures/base.ts añadiendo las fixtures como en la doc de Playwright (`test.extend({...})`).
2. La fixture `pageErrors` debe usar el hook `auto: true` y registrar listeners antes del `goto`. Después del test, llamar `expect(filtered).toEqual([])` con allowlist (`ERR_ABORTED`, `Failed to load resource`, `AbortError`, ResizeObserver loop noise).
3. Agrega también una `freezeTime` fixture: ejecuta `page.addInitScript` para fijar `Date.now()` y `new Date()` a `E2E_NOW`. Opt-in via `test.use({ freezeTime: true })`.
4. Documenta el nuevo contrato en apps/web/e2e/README.md (créalo si no existe).

CRITERIOS:
- Un test mínimo que use `adminPage` y `pageErrors` pasa sin runtime errors.
- TypeScript estricto: nada de `any`.
- Los tests existentes en apps/web/e2e/*.spec.ts siguen pasando (`pnpm --filter @open-foundry/web exec playwright test`).

Reporta cambios en base.ts y cualquier ajuste que tuvieras que hacer a specs existentes.
```

## Tarea 0.4 — Helper de accesibilidad mejorado y de regresión visual

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/e2e/helpers/a11y.ts y apps/web/e2e/visual-baseline.spec.ts.

OBJETIVO 1 (a11y): Mejorar `expectNoA11yViolations` para:
- Recibir `{ include?, exclude?, rules?: { [id]: 'off' | 'warn' | 'error' } }`.
- Generar reporte JSON detallado en `test-results/a11y/<test-name>.json` por test (incluye nodo, html, summary).
- Añadir helper `auditPageA11y(page, { route, screenshot? })` que hace `goto`, espera carga, ejecuta axe y opcionalmente toma screenshot anotado.

OBJETIVO 2 (visual): Crear apps/web/e2e/helpers/visual.ts con:
- `expectScreenshot(page, name, { mask?: Locator[], threshold?: number })` wrapper sobre `expect(page).toHaveScreenshot()` con thresholds por defecto razonables (maxDiffPixelRatio: 0.01, animations: 'disabled').
- Máscaras por defecto para elementos volátiles (timestamps, IDs aleatorios, avatares cargados externamente).
- Helper `prepareForVisual(page)` que: desactiva animaciones CSS, fija viewport, espera fuentes, oculta scrollbars.

PASOS:
1. Implementa ambos helpers.
2. Actualiza visual-baseline.spec.ts para usar el nuevo wrapper.
3. Documenta cómo regenerar baselines (`--update-snapshots`) en apps/web/e2e/README.md.

CRITERIOS:
- `pnpm --filter @open-foundry/web check` pasa.
- Una prueba de smoke en home (`/`) genera screenshot baseline y a11y report sin violaciones críticas.
```

## Tarea 0.5 — Generador de "scaffold spec" por área

```
Eres Claude Code en /home/user/OpenFoundry. 

OBJETIVO: Crear un script Node `apps/web/e2e/scripts/scaffold-spec.ts` que genere un archivo `.spec.ts` estándar a partir del nombre del área. Esto acelera la creación de los ~100 specs siguientes.

ESPECIFICACIÓN:
- Uso: `pnpm --filter @open-foundry/web exec tsx e2e/scripts/scaffold-spec.ts <area> <route-path> [pageObjectName]`
- Ejemplo: `... scaffold-spec.ts datasets /datasets DatasetsListPage`
- Output: `apps/web/e2e/<area>.spec.ts` con plantilla:
  - Import `test, expect` desde `./fixtures/base`.
  - Import del Page Object especificado.
  - 3 tests por defecto: "loads without errors", "primary CTA opens modal/navigates", "list renders mocked data".
  - Stubs `TODO:` en cada test para que el dev los rellene.
- Si el archivo ya existe, fallar (no sobrescribir).

CRITERIOS:
- Generar specs de prueba para `notifications` y `favorites` con el script y verificar que pasan (con mocks vacíos por defecto).
- Documentar el uso en apps/web/e2e/README.md.
```

---

# Fase 1 — Auth completo

## Tarea 1.1 — RegisterPage E2E

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/auth/RegisterPage.tsx y apps/web/e2e/auth-smoke.spec.ts.

OBJETIVO: Crear apps/web/e2e/auth-register.spec.ts cubriendo el flujo de registro al 100%.

CASOS A CUBRIR:
1. Render inicial: todos los inputs visibles (email, password, confirm password, nombre completo, organización si aplica).
2. Validación cliente: email inválido, password débil (mostrar regla violada), password != confirm.
3. Submit exitoso: mockea POST `/api/v1/auth/register` → 201 con `{ user, tokens }`, asserta redirección a `/auth/setup` o `/`.
4. Email ya registrado: mock 409, asserta mensaje de error visible (`role="alert"` o similar).
5. Servidor 500: asserta banner de error genérico y que el formulario sigue editable.
6. Enlace "Already have an account?" navega a `/auth/login`.
7. Tab navigation: tab a través de los campos sigue orden lógico (a11y).

REQUISITOS:
- Usar `mockAuth(page, { authenticated: false })` ANTES del goto.
- Capturar el body del POST con `captureRequests` y asertar shape (`{ email, password, name, ... }`).
- Asegurar que la password NUNCA aparece en texto plano en logs (test propio).
- Ejecutar `expectNoA11yViolations(page)` al final del primer test.

CRITERIOS:
- `pnpm --filter @open-foundry/web exec playwright test auth-register.spec.ts --project=chromium` pasa.
- Cobertura mínima 7 tests.
```

## Tarea 1.2 — SetupPage E2E

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/auth/SetupPage.tsx.

OBJETIVO: apps/web/e2e/auth-setup.spec.ts cubriendo el wizard de setup inicial (tenancy, primer admin, configuración de instancia).

CASOS:
1. Wizard se renderiza con el paso 1 activo.
2. Cada paso: rellenar campos requeridos + click "Next" → avanza al siguiente.
3. Botón "Back" preserva los valores ya introducidos.
4. Validaciones por paso (campos requeridos, formatos).
5. Submit final: mock POST `/api/v1/setup/initialize` → 201, redirige a `/`.
6. Error en submit: muestra error sin perder el state del wizard.
7. Si la instancia ya está inicializada: mock GET `/api/v1/setup/status` con `{ initialized: true }`, asserta redirect a `/auth/login`.

REQUISITOS:
- Lee SetupPage.tsx para identificar el número exacto de pasos y los campos por paso.
- Mockea todos los endpoints involucrados.
- Pruebas independientes por paso (no encadenar — usar deep links si SetupPage soporta `?step=N`, o helpers que avancen N pasos).

CRITERIOS:
- ≥ 7 tests, todos pasan.
- Cubre cada paso individualmente más happy path completo.
```

## Tarea 1.3 — MfaPage E2E

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/auth/MfaPage.tsx.

OBJETIVO: apps/web/e2e/auth-mfa.spec.ts cubriendo MFA (TOTP, WebAuthn, códigos backup).

CASOS:
1. TOTP: render input 6 dígitos, submit válido → 200 → redirect a `/`. Inválido → error inline.
2. Cambiar de método: link "Use security key" muestra WebAuthn UI (mockea `navigator.credentials.get` con `page.addInitScript`).
3. WebAuthn cancelado: muestra mensaje y permite retry.
4. Códigos backup: cambia a flujo de recovery, valida código de 8-16 chars, mock 200.
5. Rate limit: tras 5 intentos fallidos mockea 429, asserta cooldown banner con countdown.
6. Logout link en MFA page funciona (limpia tokens, redirect a `/auth/login`).

REQUISITOS:
- Para WebAuthn, NO uses el browser real — mock `navigator.credentials.get` y `navigator.credentials.create` desde initScript.
- `mockAuth(page, { authenticated: false, mfaRequired: true })` (extiende mocks.ts si hace falta).

CRITERIOS:
- ≥ 6 tests pasan.
- a11y limpia.
```

## Tarea 1.4 — CallbackPage E2E (OAuth/OIDC)

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/auth/CallbackPage.tsx.

OBJETIVO: apps/web/e2e/auth-callback.spec.ts cubriendo el callback OIDC/SAML.

CASOS:
1. Callback con `?code=xxx&state=yyy` válido: mock POST `/api/v1/auth/oidc/exchange` → 200 con tokens, redirect a `/` (o `state.return_to`).
2. State mismatch: muestra error "Invalid state" sin guardar tokens.
3. Error del IdP: callback con `?error=access_denied&error_description=...` → muestra mensaje localizado.
4. Token exchange falla con 400: muestra error y botón "Try again" que vuelve a `/auth/login`.
5. Provider deshabilitado: mock IdP catalog sin el provider del callback → error.

CRITERIOS:
- ≥ 5 tests pasan.
- Verificar que tras éxito, localStorage tiene `of_access_token` y `of_refresh_token`.
```

## Tarea 1.5 — Logout y expiración de sesión

```
Eres Claude Code en /home/user/OpenFoundry. 

OBJETIVO: apps/web/e2e/auth-session.spec.ts cubriendo:
1. Logout desde el menú de usuario en el AppShell (cualquier ruta autenticada).
2. Expiración de access token: mockea cualquier `GET /api/v1/...` con 401, asserta refresh automático via `/api/v1/auth/refresh`. Si refresh también 401, redirige a `/auth/login?return_to=...`.
3. Token revocado en servidor: 401 sin posibilidad de refresh → logout + toast "Tu sesión ha expirado".
4. Botón "Logout" llama POST `/api/v1/auth/logout` y limpia localStorage.
5. Logout en una pestaña propaga a otras (broadcastchannel/storage event) — abre dos contextos y verifica.

REQUISITOS:
- Lee el AppShell para encontrar el menú de usuario / botón de logout.
- Caso 5 usa `browser.newContext()` para simular múltiples pestañas.
```

---

# Fase 2 — Workspace core

## Tarea 2.1 — Home page

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/Home.tsx (o donde resuelva la ruta `/`) y todos los componentes que renderiza.

OBJETIVO: apps/web/e2e/home.spec.ts. Home suele ser un dashboard de bienvenida con widgets (recientes, favoritos, búsqueda rápida, accesos directos).

CASOS:
1. Render: AppShell visible (sidebar + topbar), título correcto, widgets cargan mocked data.
2. Cada widget: navega al destino esperado al hacer click en su CTA primaria.
3. Empty states: cada widget con data vacía muestra empty state legible.
4. Error en cualquier widget: muestra inline error sin romper la página.
5. Quick search (si existe en home): typear → muestra dropdown con resultados mockeados → seleccionar navega.
6. Accesos rápidos (botones "Create dataset", "Create pipeline", etc.) abren los modales/rutas correctos.

REQUISITOS:
- Mockea endpoints: `/api/v1/recent`, `/api/v1/favorites`, `/api/v1/search/suggestions`, etc. Lee el código para confirmar paths.
- Cobertura mínima 6 tests + 1 test a11y.
```

## Tarea 2.2 — Search page

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/search/SearchPage.tsx.

OBJETIVO: apps/web/e2e/search.spec.ts.

CASOS:
1. Acceso directo a `/search`: input de búsqueda focuseado.
2. Buscar string corto (<2 chars): no dispara request.
3. Buscar string válido: debounce → request `/api/v1/search?q=...`, render resultados agrupados por tipo (datasets, projects, etc.).
4. Filtros por tipo: click en chip "Datasets" → re-request con `?types=dataset`.
5. Resultado: click navega al detalle correcto (verifica URL).
6. Resultado paginado: scroll/load more carga siguiente página (`next_cursor`).
7. Empty: query sin resultados muestra empty state con sugerencias.
8. Error: 500 muestra error con retry.
9. Query syntax avanzada (si soportada): `tag:foo owner:bar`.
10. Atajo de teclado (`/` o `Cmd+K`) abre/focus search desde cualquier ruta.

REQUISITOS:
- Mockea `/api/v1/search` con respuesta tipada (lee `lib/api/search.*`).
- ≥ 8 tests.
```

## Tarea 2.3 — Notifications page

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/notifications/NotificationsPage.tsx.

OBJETIVO: apps/web/e2e/notifications.spec.ts.

CASOS:
1. Lista de notificaciones cargada con mock (mix de leídas/no leídas).
2. Filtro "Unread" oculta las leídas.
3. Marcar como leída individual: click toggle → PATCH endpoint correcto, badge en sidebar decrementa.
4. Marcar todas como leídas: POST `/api/v1/notifications/mark-all-read`.
5. Click en notificación: navega al recurso enlazado y marca como leída.
6. Empty state: 0 notificaciones.
7. Real-time (si hay websocket/SSE): mockea evento entrante con `page.evaluate` que despache un mensaje al cliente WS, asserta que aparece en lista.
8. Settings: link a /settings#notifications funciona.

REQUISITOS:
- Capturar PATCH/POST con `captureRequests` y validar payloads.
- ≥ 7 tests.
```

## Tarea 2.4 — Favorites page

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/favorites/FavoritesPage.tsx.

OBJETIVO: apps/web/e2e/favorites.spec.ts.

CASOS:
1. Lista de favoritos mockeada renderiza.
2. Filtro por tipo (dataset, project, pipeline).
3. Quitar favorito: click ★ activa → DELETE `/api/v1/favorites/:id` → item desaparece.
4. Añadir desde otra página (deeplink al recurso, marcar ★) y volver: aparece.
5. Empty state.
6. Sort: por fecha, por nombre.
7. Búsqueda dentro de favoritos.

CRITERIOS: ≥ 6 tests.
```

## Tarea 2.5 — Recent page

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/recent/RecentPage.tsx.

OBJETIVO: apps/web/e2e/recent.spec.ts.

CASOS:
1. Lista de items recientes mockeada.
2. Agrupado por día (Hoy, Ayer, Esta semana).
3. Filtro por tipo.
4. Click → navega al recurso.
5. Empty state.
6. Botón "Clear recent" (si existe): DELETE `/api/v1/recent` + confirm modal.

CRITERIOS: ≥ 5 tests.
```

---

# Fase 3 — Projects

## Tarea 3.1 — Projects list

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/projects/ProjectsListPage.tsx y apps/web/e2e/pages/ProjectsPage.ts.

OBJETIVO: apps/web/e2e/projects-list.spec.ts.

CASOS:
1. Render con N proyectos mockeados.
2. Crear proyecto: botón "New project" abre modal, rellenar nombre/descripción, submit → POST `/api/v1/projects`, asserta toast éxito y nueva fila en lista.
3. Validación: nombre vacío bloquea submit.
4. Editar nombre inline o vía menú "..." → PATCH.
5. Eliminar: menú "..." > Delete → confirm modal con "type project name to confirm" → DELETE.
6. Búsqueda/filtro local.
7. Ordenar por nombre, fecha, owner.
8. Paginación.
9. Click en fila → navega a `/projects/:id`.
10. Permisos: con `viewerPage` el botón "New project" no aparece.

CRITERIOS: ≥ 9 tests.
```

## Tarea 3.2 — Project detail

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/projects/ProjectDetailPage.tsx (2425 líneas — léelo por chunks).

OBJETIVO: apps/web/e2e/project-detail.spec.ts.

CASOS:
1. Render: breadcrumb, título, tabs (Files, Members, Settings, Activity).
2. Tab Files: árbol de carpetas/archivos mockeado renderiza.
3. Crear carpeta: botón → input inline → submit → POST.
4. Crear archivo (dataset, notebook, etc.) desde "+ New": cada opción abre el flujo correcto.
5. Mover items: drag-drop (usar `dragTo`) entre carpetas.
6. Renombrar inline.
7. Eliminar (single + bulk).
8. Tab Members: lista de miembros, añadir/quitar miembro, cambiar rol.
9. Tab Settings: editar nombre, descripción, visibilidad; eliminar proyecto.
10. Tab Activity: timeline de eventos mockeado.
11. Botón "Star" añade a favoritos.
12. Compartir: modal con enlace + permisos.

CRITERIOS: ≥ 10 tests. Para drag-drop usa `page.locator(src).dragTo(page.locator(dst))`.
```

## Tarea 3.3 — Project folder

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/projects/ProjectFolderPage.tsx.

OBJETIVO: apps/web/e2e/project-folder.spec.ts.

CASOS:
1. Navega a `/projects/:projectId/folders/:folderId`: breadcrumb correcto con segments clickables.
2. Click en breadcrumb sube de nivel.
3. Listado de items + acciones (idéntico a detail tab Files).
4. Subir archivo (file input) → POST upload mockeado.
5. Navegación profunda (3+ niveles).
6. Folder vacío muestra empty state.

CRITERIOS: ≥ 5 tests.
```

---

# Fase 4 — Datasets

## Tarea 4.1 — Datasets list

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/datasets/DatasetsListPage.tsx y apps/web/src/lib/api/datasets*.

OBJETIVO: apps/web/e2e/datasets-list.spec.ts.

CASOS:
1. Render con N datasets.
2. Filtros faceted: por owner, formato (parquet, csv, iceberg), tag.
3. Búsqueda por nombre.
4. Ordenar.
5. Botón "Upload": navega a `/datasets/upload`.
6. Click en fila → `/datasets/:id`.
7. Crear dataset vacío (si existe acción).
8. Bulk select + delete.
9. Cambiar vista (grid/list) si el toggle existe.
10. Paginación con `next_cursor`.
11. Empty state.

CRITERIOS: ≥ 9 tests.
```

## Tarea 4.2 — Dataset upload

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/datasets/DatasetUploadPage.tsx.

OBJETIVO: apps/web/e2e/dataset-upload.spec.ts.

CASOS:
1. Render wizard: paso "Select file".
2. Subir archivo válido (`page.setInputFiles` con un fixture CSV pequeño en apps/web/e2e/fixtures/files/sample.csv — créalo).
3. Subir archivo no soportado → error.
4. Avanzar a paso "Schema": preview de columnas con tipos auto-detectados editable.
5. Editar tipo de columna (dropdown).
6. Paso "Metadata": nombre, descripción, tags, proyecto destino.
7. Submit final: POST multipart, asserta progress bar y redirect a detail.
8. Cancelar mid-upload: abort + cleanup.
9. Validación: nombre duplicado en proyecto → error inline.

CRITERIOS: ≥ 8 tests. Crear el fixture CSV si no existe.
```

## Tarea 4.3 — Dataset detail

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/datasets/DatasetDetailPage.tsx.

OBJETIVO: apps/web/e2e/dataset-detail.spec.ts.

CASOS:
1. Render tabs: Preview, Schema, Branches, Transactions, Permissions, Settings.
2. Tab Preview: tabla con filas mockeadas (paginación, sort por columna).
3. Tab Schema: lista de columnas con tipos; editar (si permitido) → PATCH.
4. Tab Branches: lista, crear branch nueva → POST.
5. Tab Transactions: timeline.
6. Tab Permissions: añadir miembro + rol.
7. Tab Settings: rename, delete, archive.
8. Acción "Download": click → request a `/api/v1/datasets/:id/export` (mock con Blob).
9. Botón "Edit in Quiver / Notebook" navega.
10. Versión histórica (dropdown de transaction): cambiar selección recarga preview.

CRITERIOS: ≥ 9 tests.
```

## Tarea 4.4 — Dataset branches list

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/datasets/DatasetBranchesPage.tsx.

OBJETIVO: apps/web/e2e/dataset-branches.spec.ts.

CASOS:
1. Lista de branches con estado (active, merged, abandoned).
2. Crear branch desde main: modal con nombre + base branch → POST.
3. Merge branch: botón "Merge to main" → confirm con preview de conflictos → POST.
4. Abandonar branch: confirm → PATCH `{ status: 'abandoned' }`.
5. Filtro por estado.
6. Click en branch → detail.

CRITERIOS: ≥ 5 tests.
```

## Tarea 4.5 — Dataset branch detail

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/datasets/DatasetBranchDetailPage.tsx.

OBJETIVO: apps/web/e2e/dataset-branch-detail.spec.ts.

CASOS:
1. Render: nombre branch, base, divergencia (X commits ahead/behind).
2. Diff con main: lista de cambios (filas añadidas/modificadas/eliminadas).
3. Acción "Promote to main".
4. Crear commit (si la UI lo permite): mensaje + archivos modificados.
5. Comparación entre dos branches arbitrarias.

CRITERIOS: ≥ 4 tests.
```

---

# Fase 5 — Tablas y objetos

## Tarea 5.1 — Virtual Tables (list + detail)

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/virtual-tables/*.

OBJETIVO: apps/web/e2e/virtual-tables.spec.ts (cubre list y detail en un solo spec).

CASOS list:
1. Render lista con N tablas.
2. Crear virtual table: botón → modal con nombre + SQL/definition → POST.
3. Validación SQL: error inline si parse falla (mock validate endpoint).
4. Editar metadata inline.
5. Eliminar con confirm.
6. Filtro por fuente.

CASOS detail (`/virtual-tables/:rid`):
7. Preview de datos.
8. Edit query (Monaco editor) → "Validate" → mock response → "Save".
9. Schedule refresh: cron picker.
10. View lineage (botón abre lineage page).

CRITERIOS: ≥ 9 tests.
```

## Tarea 5.2 — Iceberg Tables (list + detail)

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/iceberg-tables/*.

OBJETIVO: apps/web/e2e/iceberg-tables.spec.ts.

CASOS list:
1. Lista con namespace/table.
2. Crear: modal con namespace, name, schema (JSON editor o form builder).
3. Filtro por namespace.
4. Click → detail.

CASOS detail:
5. Tabs: Schema, Snapshots, Partitions, Properties.
6. Snapshots timeline: rollback action.
7. Editar schema (add/remove column).
8. Partitions: estadísticas y compaction action.
9. Properties: key/value editor.

CRITERIOS: ≥ 8 tests.
```

## Tarea 5.3 — Object Databases

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/object-databases/ObjectDatabasesPage.tsx.

OBJETIVO: apps/web/e2e/object-databases.spec.ts.

CASOS:
1. Lista de databases.
2. Crear database: modal con nombre, descripción, ontología asociada.
3. Configurar conexión: form con tipo (Postgres, Cassandra, etc.), credenciales, test connection (mock).
4. Test connection success/failure.
5. Edit/Delete.
6. Status indicator (connected/disconnected) refleja mock.

CRITERIOS: ≥ 5 tests.
```

## Tarea 5.4 — Object Explorer

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/object-explorer/ObjectExplorerPage.tsx.

OBJETIVO: apps/web/e2e/object-explorer.spec.ts.

CASOS:
1. Selector de Object Type (dropdown con types mockeados).
2. Tras seleccionar: tabla de objetos con paginación.
3. Filtro por propiedad (input + operador).
4. Click en objeto → drawer con detalle + properties + links.
5. Editar property inline (si permitido) → PATCH.
6. Crear nuevo objeto: botón "+ New" abre form basado en schema del type.
7. Eliminar objeto.
8. Exportar resultados (CSV).
9. Guardar query como Object Set.

CRITERIOS: ≥ 8 tests.
```

## Tarea 5.5 — Object Views

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/object-views/ObjectViewsPage.tsx.

OBJETIVO: apps/web/e2e/object-views.spec.ts.

CASOS:
1. Lista de vistas.
2. Crear vista: nombre + base object type + filtros + columnas seleccionadas.
3. Editar.
4. Duplicar.
5. Compartir / permisos.
6. Marcar como vista pública.
7. Eliminar.
8. Preview de la vista antes de guardar.

CRITERIOS: ≥ 6 tests.
```

## Tarea 5.6 — Object Monitors

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/object-monitors/ObjectMonitorsPage.tsx.

OBJETIVO: apps/web/e2e/object-monitors.spec.ts.

CASOS:
1. Lista de monitors (alertas sobre objetos).
2. Crear monitor: object set + condición + acción (notificar, email, webhook).
3. Cron schedule picker.
4. Pausar/reanudar monitor.
5. Ver historial de disparos.
6. Eliminar.

CRITERIOS: ≥ 5 tests.
```

## Tarea 5.7 — Object Link Types

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/object-link-types/ObjectLinkTypesPage.tsx.

OBJETIVO: apps/web/e2e/object-link-types.spec.ts.

CASOS:
1. Lista de link types entre object types.
2. Crear: source type, target type, cardinality (1:1, 1:N, N:M), nombre del link.
3. Editar.
4. Eliminar (warning si está en uso).
5. Validación: tipos requeridos.

CRITERIOS: ≥ 4 tests.
```

---

# Fase 6 — Pipelines, Schedules, Builds, Foundry Rules

## Tarea 6.1 — Pipelines list

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/pipelines/PipelinesPage.tsx.

OBJETIVO: apps/web/e2e/pipelines-list.spec.ts.

CASOS:
1. Tabs por estado: All / Draft / Active / Paused / Archived.
2. Cada tab filtra correctamente.
3. Búsqueda por nombre.
4. Filtros: owner, tag, schedule.
5. Botón "New pipeline" → `/pipelines/new`.
6. Click fila → `/pipelines/:id/edit`.
7. Acciones rápidas en fila: pausar, reanudar, archivar.
8. Bulk select + bulk archive.
9. Ordenar por última ejecución, próximo trigger, etc.

CRITERIOS: ≥ 8 tests.
```

## Tarea 6.2 — Pipeline new (wizard)

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/pipelines/PipelineNewPage.tsx.

OBJETIVO: apps/web/e2e/pipeline-new.spec.ts.

CASOS:
1. Render wizard.
2. Paso 1: metadata (nombre, descripción, proyecto).
3. Paso 2: seleccionar dataset(s) de entrada.
4. Paso 3: seleccionar transformación / código.
5. Paso 4: configurar output dataset.
6. Paso 5: schedule opcional.
7. Submit: POST `/api/v1/pipelines` → redirect a editor.
8. Validación por paso.
9. Botón "Save as draft" sin completar todos los pasos.

CRITERIOS: ≥ 7 tests.
```

## Tarea 6.3 — Pipeline edit (DAG editor)

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/pipelines/PipelineEditPage.tsx (probablemente render Cytoscape o React Flow).

OBJETIVO: apps/web/e2e/pipeline-edit.spec.ts.

CASOS:
1. Render del DAG con N nodos mockeados.
2. Click en nodo → panel lateral con properties.
3. Editar property → PATCH.
4. Añadir nodo: drag desde palette → drop en canvas (usar `dragTo`).
5. Conectar dos nodos (drag desde handle de salida a handle de entrada).
6. Eliminar nodo: select + Delete key (`page.keyboard.press('Delete')`).
7. Botón "Run now": POST `/api/v1/pipelines/:id/run` → toast + nueva fila en runs.
8. Tab "Runs": lista de ejecuciones, click → log viewer.
9. Tab "Logs": filtro por nivel, búsqueda.
10. Versionado: ver versión anterior, restore.
11. Validar pipeline: botón "Validate" → mock 200/422.
12. Para grafos: WORKSHOP CSS classes son inestables. Usa `data-testid="pipeline-node-<id>"`.

CRITERIOS: ≥ 10 tests. Documenta data-testids requeridos si faltan en el código fuente y abre una issue para añadirlos.
```

## Tarea 6.4 — Pipeline linter

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/pipeline-linter/LinterPage.tsx.

OBJETIVO: apps/web/e2e/pipeline-linter.spec.ts.

CASOS:
1. Lista de issues lint con severidad (error, warning, info).
2. Filtro por severidad.
3. Filtro por pipeline.
4. Click en issue → drawer con detalle + sugerencia de fix.
5. Auto-fix (botón si la sugerencia es aplicable) → PATCH.
6. Reglas habilitadas/deshabilitadas (config).

CRITERIOS: ≥ 5 tests.
```

## Tarea 6.5 — Foundry Rules / Workflows

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/foundry-rules/FoundryRulesPage.tsx.

OBJETIVO: apps/web/e2e/foundry-rules.spec.ts.

CASOS:
1. Lista de rules.
2. Crear rule: trigger (event, schedule, manual) + condiciones + acciones.
3. Builder condicional: añadir/quitar condiciones, operadores AND/OR.
4. Acciones: send notification, run pipeline, call webhook.
5. Test rule: botón "Dry run" con input → muestra qué se dispararía.
6. Activar/desactivar.
7. Historial de ejecuciones.
8. Eliminar.
9. Redirects legacy: `/workflows` y `/automate` deben llevar a `/foundry-rules`.

CRITERIOS: ≥ 7 tests.
```

## Tarea 6.6 — Schedules

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/schedules/*.

OBJETIVO: apps/web/e2e/schedules.spec.ts (cubre NewSchedulePage y ScheduleDetailPage).

CASOS:
1. Crear schedule: nombre + cron expression + recurso target.
2. Validación cron (preview de próximas ejecuciones).
3. Detail: pausar, reanudar, eliminar.
4. Historial.
5. Editar cron y target.
6. Timezone picker.

CRITERIOS: ≥ 5 tests.
```

## Tarea 6.7 — Build Schedules + Sweep

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/build-schedules/*.

OBJETIVO: apps/web/e2e/build-schedules.spec.ts.

CASOS:
1. Lista de build schedules.
2. Crear: target pipeline(s) + cron + retry config.
3. Editar.
4. SweepPage (`/build-schedules/sweep`): selector de schedules + acción bulk (pausar, eliminar) + confirm.
5. Dry-run del sweep antes de aplicar.

CRITERIOS: ≥ 4 tests.
```

## Tarea 6.8 — Builds list + detail

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/builds/*.

OBJETIVO: apps/web/e2e/builds.spec.ts.

CASOS:
1. Lista de builds con estado (queued, running, success, failed).
2. Filtro por estado, pipeline, fecha.
3. Click → detail.
4. Detail: timeline de pasos, logs por step (toggle expand), artifacts.
5. Acción "Retry" en build fallido → POST.
6. Cancelar build running → POST `/cancel`.
7. Download logs.
8. Real-time updates (mock SSE/WS para builds running).

CRITERIOS: ≥ 7 tests.
```

---

# Fase 7 — Ontology completa

## Tarea 7.1 — Ontology Manager

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/ontology-manager/OntologyManagerPage.tsx.

OBJETIVO: apps/web/e2e/ontology-manager.spec.ts.

CASOS:
1. Lista de ontologías + tipos.
2. Crear ontología.
3. Importar/exportar (JSON).
4. Cambiar versión.
5. Publish ontology (draft → published).
6. Diff entre versiones.

CRITERIOS: ≥ 5 tests.
```

## Tarea 7.2 — Bindings Wizard

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/ontology-manager/BindingsWizardPage.tsx.

OBJETIVO: apps/web/e2e/ontology-bindings.spec.ts.

CASOS:
1. Render wizard.
2. Paso "Select source dataset".
3. Paso "Map columns to properties" (drag-drop o dropdowns).
4. Validación: propiedades requeridas mapeadas.
5. Preview de objetos resultantes.
6. Submit: crea binding.

CRITERIOS: ≥ 5 tests.
```

## Tarea 7.3 — Ontology Home + Graph

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/ontology/OntologyHomePage.tsx y OntologyGraphPage.tsx.

OBJETIVO: apps/web/e2e/ontology-home-graph.spec.ts.

CASOS Home:
1. Render con stats (#types, #properties, #links).
2. Navegación a sub-secciones.

CASOS Graph (Cytoscape):
3. Render del grafo con N tipos.
4. Click en nodo → panel con info.
5. Zoom in/out (botones).
6. Layout picker (force, hierarchical, etc.).
7. Filtro por namespace.
8. Cytoscape suele NO ser accesible: añade `<style>` y `data-testid` a través del wrapper React. Si no es posible, hacer assertions vía API del componente (`window.__cy__`) expuesta solo en modo test.

CRITERIOS: ≥ 5 tests.
```

## Tarea 7.4 — Create Object Type

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/ontology/CreateObjectTypePage.tsx.

OBJETIVO: apps/web/e2e/ontology-create-type.spec.ts.

CASOS:
1. Form: nombre, plural, descripción, icono, color.
2. Añadir property: tipo (string, number, date, etc.), required, default.
3. Añadir múltiples properties.
4. Primary key picker.
5. Validación de nombres únicos.
6. Submit → POST.

CRITERIOS: ≥ 5 tests.
```

## Tarea 7.5 — Object Sets

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/ontology/ObjectSetsPage.tsx.

OBJETIVO: apps/web/e2e/object-sets.spec.ts.

CASOS:
1. Lista de object sets.
2. Crear desde query builder.
3. Operaciones de conjunto: union, intersect, except entre object sets.
4. Materializar (snapshot).
5. Eliminar.

CRITERIOS: ≥ 4 tests.
```

## Tarea 7.6 — Object Type Detail

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/ontology/ObjectTypeDetailPage.tsx.

OBJETIVO: apps/web/e2e/object-type-detail.spec.ts.

CASOS:
1. Tabs: Overview, Properties, Links, Permissions, Audit.
2. Editar property: tipo, required, validation.
3. Añadir/quitar link a otro type.
4. Permisos: roles que pueden read/write.
5. Audit log con cambios históricos.
6. Eliminar object type (con check de uso).

CRITERIOS: ≥ 6 tests.
```

---

# Fase 8 — Apps y Workshop (cobertura ampliada)

## Tarea 8.1 — Apps list

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/apps/AppsPage.tsx.

OBJETIVO: apps/web/e2e/apps-list.spec.ts.

CASOS:
1. Lista de apps con thumbnail + metadata.
2. Crear app: nombre, template (blank, from existing).
3. Duplicar app.
4. Publicar/despublicar.
5. Compartir.
6. Eliminar.
7. Filtros por estado (draft, published).
8. Búsqueda.

CRITERIOS: ≥ 6 tests.
```

## Tarea 8.2 — Workshop Editor (CORE — ampliar cobertura existente)

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/apps/WorkshopEditorPage.tsx (4699 líneas, leerlo por chunks de 500 líneas). Ya existen ~20 specs workshop-* en apps/web/e2e/ que cubren el RUNTIME (`/apps/runtime/:slug`). El EDITOR (`/apps/:id/workshop`) está infra-cubierto.

OBJETIVO: apps/web/e2e/workshop-editor.spec.ts (NUEVO — solo editor, no runtime).

CASOS:
1. Abrir app existente: canvas vacío + palette de widgets visible.
2. Drag-drop widget desde palette al canvas → instancia con id único.
3. Click en widget → panel de properties.
4. Editar property text: ver cambio reflejado en preview.
5. Bindings: link property a variable o output de otro widget.
6. Eventos: añadir evento on-click → acción (navigate, run action type, set variable).
7. Variables: crear variable, set default, leer en widget.
8. Layout: añadir sección, columnas/filas, tabs, loop.
9. Code blocks (Monaco): editar query, validar syntax.
10. Preview mode: toggle entre edit y preview, asserta que botones interactivos disparan eventos.
11. Save: ctrl+s o botón → PUT, asserta toast.
12. Undo/redo (ctrl+z, ctrl+shift+z).
13. Publish: cambia estado draft → published.
14. Versionado: ver versión anterior y revert.
15. Multi-user / locking (si aplica).

CRITERIOS: ≥ 12 tests. Para Monaco, usa `page.locator('.monaco-editor textarea').first().fill(text)`.
```

## Tarea 8.3 — App Runtime edge cases

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/apps/AppRuntimePage.tsx. Ya hay specs workshop-* cubriendo runtime para apps definidas; queremos cubrir EDGE CASES.

OBJETIVO: apps/web/e2e/app-runtime-edges.spec.ts.

CASOS:
1. Slug inexistente → 404 page.
2. App despublicada → 403 con mensaje "App is not published".
3. App publicada pero sin permisos → 403 con CTA "Request access".
4. Carga lenta (mock latencia 3s) → loading state visible, no flash de contenido.
5. Error en uno de los widgets → otros siguen funcionando, widget errado muestra error inline.
6. Mobile viewport: layout colapsa a single column.
7. Deeplink a una página específica del app (`/apps/runtime/:slug?page=detail&id=X`) carga directo.

CRITERIOS: ≥ 6 tests.
```

---

# Fase 9 — Data Connection

## Tarea 9.1 — Data Connection home

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/data-connection/DataConnectionPage.tsx.

OBJETIVO: apps/web/e2e/data-connection.spec.ts.

CASOS:
1. Lista de sources con tipo, estado, último sync.
2. Filtro por tipo.
3. Click en source → `/data-connection/sources/:id`.
4. Nuevo source button → `/data-connection/new`.
5. Botones de acción rápida: pausar sync, sync now.
6. Empty state.

CRITERIOS: ≥ 5 tests.
```

## Tarea 9.2 — Agents

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/data-connection/AgentsPage.tsx.

OBJETIVO: apps/web/e2e/data-connection-agents.spec.ts.

CASOS:
1. Lista de agents con estado (online/offline) y heartbeat.
2. Registrar nuevo agent: modal con token de instalación.
3. Editar agent: nombre, tags.
4. Eliminar.
5. Ver logs del agent.

CRITERIOS: ≥ 4 tests.
```

## Tarea 9.3 — Egress Policies

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/data-connection/EgressPoliciesPage.tsx.

OBJETIVO: apps/web/e2e/egress-policies.spec.ts.

CASOS:
1. Lista de políticas.
2. Crear policy: nombre + reglas (hosts permitidos, puertos, protocolos).
3. Asignar a agent o source.
4. Editar.
5. Test policy (dry-run).
6. Eliminar.

CRITERIOS: ≥ 5 tests.
```

## Tarea 9.4 — New Source / New Streaming Source

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/data-connection/NewSourcePage.tsx y NewStreamingSourcePage.tsx.

OBJETIVO: apps/web/e2e/data-connection-new.spec.ts.

CASOS NewSource:
1. Catálogo de connectors (Postgres, S3, MySQL, etc.).
2. Seleccionar connector → form de credenciales específico.
3. Test connection → mock 200/400.
4. Avanzar a "Select tables" → checklist.
5. Schedule de sync.
6. Submit → POST.

CASOS NewStreamingSource:
7. Catálogo de streaming (Kafka, Kinesis, PubSub).
8. Config específica por tipo.
9. Test consumer.
10. Submit.

CRITERIOS: ≥ 8 tests.
```

## Tarea 9.5 — Source detail

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/data-connection/SourceDetailPage.tsx.

OBJETIVO: apps/web/e2e/source-detail.spec.ts.

CASOS:
1. Tabs: Overview, Tables, Sync history, Schema, Settings.
2. Sync now button.
3. Pausar/reanudar sync.
4. Editar credenciales (con re-test).
5. Eliminar source (con warning de datasets dependientes).
6. Ver logs de sync.

CRITERIOS: ≥ 5 tests.
```

---

# Fase 10 — AI

## Tarea 10.1 — AI home

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/ai/AiPage.tsx.

OBJETIVO: apps/web/e2e/ai-home.spec.ts.

CASOS:
1. Hub con tiles a sub-secciones (Assist, Analyst, Threads, Documents, ChatBot Studio, Operator).
2. Cada tile navega correctamente.
3. Estadísticas: usage tokens, threads activos.

CRITERIOS: ≥ 3 tests.
```

## Tarea 10.2 — Assist

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/ai/AssistPage.tsx.

OBJETIVO: apps/web/e2e/ai-assist.spec.ts.

CASOS:
1. Render chat panel + sugerencias iniciales.
2. Enviar mensaje: input + Enter → POST `/api/v1/ai/assist/chat` (mock streaming SSE).
3. Mock SSE chunks: usa `page.route` con `Content-Type: text/event-stream` y emite chunks.
4. Cancelar respuesta mid-stream (botón Stop).
5. Copiar mensaje al portapapeles (verificar con `navigator.clipboard.readText` mockeado).
6. Rate limit handling.
7. Adjuntar contexto: dataset, object, document.
8. New chat button limpia historial.

CRITERIOS: ≥ 7 tests. Documenta el patrón de mock SSE en apps/web/e2e/README.md para reutilizar.
```

## Tarea 10.3 — Analyst

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/ai/AnalystPage.tsx.

OBJETIVO: apps/web/e2e/ai-analyst.spec.ts.

CASOS:
1. Render interface NL → SQL/data.
2. Input query en lenguaje natural → mock response con SQL generado + preview.
3. Editar SQL antes de ejecutar.
4. Ejecutar → tabla resultado.
5. Guardar como Object Set / Saved query.
6. Histórico de queries.

CRITERIOS: ≥ 5 tests.
```

## Tarea 10.4 — Threads

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/ai/ThreadsPage.tsx.

OBJETIVO: apps/web/e2e/ai-threads.spec.ts.

CASOS:
1. Sidebar con lista de threads.
2. Crear thread nuevo.
3. Selectar thread → render mensajes.
4. Renombrar thread.
5. Eliminar thread.
6. Búsqueda de threads.
7. Compartir thread (genera link mock).

CRITERIOS: ≥ 6 tests.
```

## Tarea 10.5 — Documents

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/ai/DocumentsPage.tsx.

OBJETIVO: apps/web/e2e/ai-documents.spec.ts.

CASOS:
1. Lista de documentos indexados para RAG.
2. Upload documento (PDF, DOCX, TXT) → `page.setInputFiles`.
3. Estado de indexing (queued, indexing, ready, failed).
4. Reindex action.
5. Delete document.
6. Vista previa del contenido.
7. Búsqueda semántica dentro de docs.

CRITERIOS: ≥ 6 tests.
```

## Tarea 10.6 — ChatBot Studio

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/ai/ChatbotStudioPage.tsx.

OBJETIVO: apps/web/e2e/ai-chatbot-studio.spec.ts.

CASOS:
1. Lista de bots configurados.
2. Crear bot: nombre, system prompt, modelo, tools/funciones disponibles.
3. Test playground: chat con el bot recién creado.
4. Versioning del bot.
5. Publicar bot (genera endpoint público mock).
6. Eliminar.

CRITERIOS: ≥ 5 tests.
```

## Tarea 10.7 — Operator

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/ai/OperatorPage.tsx.

OBJETIVO: apps/web/e2e/ai-operator.spec.ts.

CASOS:
1. Lista de operator runs (agentes autonomos).
2. Nuevo run: descripción de la tarea + recursos permitidos.
3. Ver progreso en tiempo real (SSE mock).
4. Pausar / cancelar.
5. Ver pasos ejecutados.
6. Ver tools usados.

CRITERIOS: ≥ 5 tests.
```

---

# Fase 11 — Functions y Action Types

## Tarea 11.1 — Functions / Compute Modules

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/functions/FunctionsPage.tsx.

OBJETIVO: apps/web/e2e/functions.spec.ts.

CASOS:
1. Lista de funciones.
2. Crear función: nombre, runtime (Python, Go, etc.), código (Monaco), inputs/outputs schema.
3. Test function: input JSON → mock execute response.
4. Versioning.
5. Deploy / undeploy.
6. Logs de ejecuciones.
7. Eliminar.

CRITERIOS: ≥ 6 tests.
```

## Tarea 11.2 — Action Types list

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/action-types/ActionTypesPage.tsx (2428 líneas).

OBJETIVO: apps/web/e2e/action-types-list.spec.ts.

CASOS:
1. Lista con N action types.
2. Filtrar por object type.
3. Crear: wizard (nombre, display, object type, params, function/SQL).
4. Editar.
5. Versioning.
6. Eliminar.
7. Test action: input form → mock validate/execute.

CRITERIOS: ≥ 6 tests.
```

## Tarea 11.3 — Action Type detail

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/action-types/ActionTypeDetailPage.tsx.

OBJETIVO: apps/web/e2e/action-type-detail.spec.ts.

CASOS:
1. Tabs: Overview, Parameters, Behavior, Permissions, History.
2. Editar param: tipo, required, default, validation rule.
3. Editar behavior (código): Monaco editor.
4. Permisos por rol.
5. History con diffs.

CRITERIOS: ≥ 5 tests.
```

---

# Fase 12 — Lineage y Visualización

## Tarea 12.1 — Lineage

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/lineage/LineagePage.tsx (2252 líneas).

OBJETIVO: apps/web/e2e/lineage.spec.ts.

CASOS:
1. Selector de recurso de entrada (dataset, pipeline, object type).
2. Renderiza grafo (Cytoscape o ReactFlow) con upstream/downstream.
3. Toggle direction: upstream only, downstream only, both.
4. Profundidad ajustable (slider).
5. Click en nodo → drawer con info.
6. Resaltar camino entre dos nodos.
7. Exportar como imagen / JSON.
8. Filtros por tipo de recurso.

CRITERIOS: ≥ 6 tests. Para grafo, mismo patrón que en Tarea 6.3 (data-testids o API expuesta).
```

## Tarea 12.2 — Quiver

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/quiver/QuiverPage.tsx.

OBJETIVO: apps/web/e2e/quiver.spec.ts.

CASOS:
1. Render canvas con widgets configurables (charts).
2. Añadir chart: tipo (bar, line, pie, etc.), data source.
3. Configurar dimensiones/medidas.
4. Drill-down al hacer click en barra.
5. Filtros globales.
6. Save dashboard.
7. Export PNG / PDF (mock).
8. Refresh data.

CRITERIOS: ≥ 6 tests.
```

## Tarea 12.3 — Geospatial

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/geospatial/GeospatialPage.tsx.

OBJETIVO: apps/web/e2e/geospatial.spec.ts.

CASOS:
1. Render MapLibre canvas.
2. Cargar layer (mock GeoJSON via route).
3. Cambiar basemap.
4. Zoom in/out + pan (`mouse.wheel`, `mouse.down/move`).
5. Filtros por bounding box.
6. Click en feature → popup.
7. Toggle visibility de layer.
8. Drawer de leyenda.

CRITERIOS: ≥ 6 tests. MapLibre es un canvas — usa data-testids en controles UI y APIs expuestas (`window.__map__`) para interacciones de mapa puro.
```

## Tarea 12.4 — Contour

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/contour/ContourPage.tsx.

OBJETIVO: apps/web/e2e/contour.spec.ts.

CASOS:
1. Render canvas de análisis.
2. Crear workflow de análisis: bloque de query → bloque de transform → bloque de viz.
3. Drag-drop bloques.
4. Conectar bloques.
5. Ejecutar workflow.
6. Guardar como vista.

CRITERIOS: ≥ 5 tests.
```

## Tarea 12.5 — Vertex

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/vertex/VertexPage.tsx.

OBJETIVO: apps/web/e2e/vertex.spec.ts.

CASOS:
1. Vista de grafo de objetos.
2. Buscar y centrar nodo.
3. Expandir relaciones (links) del nodo.
4. Filtros por tipo.
5. Layout picker.
6. Path finder entre dos nodos.

CRITERIOS: ≥ 5 tests.
```

---

# Fase 13 — Notebooks, Notepad, Slate

## Tarea 13.1 — Slate (publishing)

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/slate/SlatePage.tsx.

OBJETIVO: apps/web/e2e/slate.spec.ts.

CASOS:
1. Lista de reports/posts.
2. Crear report: editor rich text (probablemente Tiptap o similar).
3. Insertar bloque (chart, table, image).
4. Publicar (estado draft → published).
5. Compartir.
6. Versioning.
7. Eliminar.

CRITERIOS: ≥ 6 tests.
```

## Tarea 13.2 — Notebooks list + detail

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/notebooks/*.

OBJETIVO: apps/web/e2e/notebooks.spec.ts.

CASOS list:
1. Lista con N notebooks.
2. Crear notebook nuevo (Python, R, SQL).
3. Filtros / búsqueda.
4. Click → detail.

CASOS detail:
5. Render celdas mockeadas.
6. Añadir celda (code, markdown).
7. Editar celda (Monaco).
8. Ejecutar celda (mock kernel response).
9. Ejecutar todas las celdas.
10. Output rendering (texto, tabla, imagen).
11. Reorder celdas (drag-drop).
12. Eliminar celda.
13. Save notebook.
14. Restart kernel.

CRITERIOS: ≥ 11 tests.
```

## Tarea 13.3 — Notepad list + detail

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/notepad/*.

OBJETIVO: apps/web/e2e/notepad.spec.ts.

CASOS:
1. Lista de notas.
2. Crear nota.
3. Editar (rich text).
4. Pin / archive.
5. Buscar.
6. Eliminar.

CRITERIOS: ≥ 5 tests.
```

---

# Fase 14 — Streaming y Media Sets

## Tarea 14.1 — Streaming list + detail

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/streaming/*.

OBJETIVO: apps/web/e2e/streaming.spec.ts.

CASOS list:
1. Lista de streams.
2. Crear stream nuevo.
3. Estado (active, paused, error) con badge.

CASOS detail:
4. Tabs: Overview, Messages, Consumers, Schema, Settings.
5. Tail messages en tiempo real (mock SSE/WS).
6. Pausar/reanudar.
7. Replay desde timestamp.
8. Schema evolution.

CRITERIOS: ≥ 7 tests.
```

## Tarea 14.2 — Media Sets

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/media-sets/*.

OBJETIVO: apps/web/e2e/media-sets.spec.ts.

CASOS list:
1. Lista de media sets.
2. Crear media set.
3. Filtros por tipo (image, video, audio).

CASOS detail:
4. Galería con thumbnails.
5. Subir archivos (file input multi).
6. Eliminar items.
7. Metadata editor.
8. Preview de imagen/video.
9. Bulk actions.

CRITERIOS: ≥ 7 tests.
```

---

# Fase 15 — Model Catalog y AIP Evals

## Tarea 15.1 — Model Catalog (ML)

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/ml/MlPage.tsx.

OBJETIVO: apps/web/e2e/model-catalog.spec.ts.

CASOS:
1. Lista de modelos con versión, framework, estado.
2. Filtros (framework, owner, deployed).
3. Detail (drawer o página): metadata, métricas, artifacts.
4. Promote version (staging → prod).
5. Rollback.
6. Deploy / undeploy.
7. Eliminar.

CRITERIOS: ≥ 6 tests.
```

## Tarea 15.2 — AIP Evals

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/aip-evals/AipEvalsPage.tsx.

OBJETIVO: apps/web/e2e/aip-evals.spec.ts.

CASOS:
1. Lista de eval suites.
2. Crear suite: nombre, dataset de prueba, métricas.
3. Ejecutar eval → mock progress + resultados.
4. Comparar runs.
5. Exportar resultados.

CRITERIOS: ≥ 4 tests.
```

---

# Fase 16 — Funcionalidad avanzada

## Tarea 16.1 — Machinery

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/machinery/MachineryPage.tsx.

OBJETIVO: apps/web/e2e/machinery.spec.ts.

CASOS:
1. Identifica el dominio (suele ser orchestración / state machines).
2. Lista de state machines.
3. Crear / editar.
4. Ver transiciones de estado.
5. Trigger transition manual.

CRITERIOS: ≥ 4 tests.
```

## Tarea 16.2 — Fusion

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/fusion/FusionPage.tsx.

OBJETIVO: apps/web/e2e/fusion.spec.ts.
CASOS: render principal, acciones principales según el código. ≥ 4 tests.
```

## Tarea 16.3 — Logic Authoring

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/logic/LogicAuthoringPage.tsx.

OBJETIVO: apps/web/e2e/logic-authoring.spec.ts.

CASOS:
1. Editor Monaco con autocomplete.
2. Crear regla.
3. Sintaxis (Cedar / DSL propio).
4. Test rule con input.
5. Save / version.
6. Lint errors inline.

CRITERIOS: ≥ 5 tests.
```

## Tarea 16.4 — Dynamic Scheduling

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/dynamic-scheduling/DynamicSchedulingPage.tsx.

OBJETIVO: apps/web/e2e/dynamic-scheduling.spec.ts.
CASOS: Configurar políticas de scheduling dinámico, ver impacto en builds, ≥ 4 tests.
```

## Tarea 16.5 — Interfaces

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/interfaces/InterfacesPage.tsx.

OBJETIVO: apps/web/e2e/interfaces.spec.ts.
CASOS: Crear interface, asignar tipos que la implementan, ≥ 4 tests.
```

## Tarea 16.6 — Code Repos

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/code-repos/CodeReposPage.tsx.

OBJETIVO: apps/web/e2e/code-repos.spec.ts.

CASOS:
1. Lista de repos conectados.
2. Conectar nuevo repo (GitHub, GitLab, etc.) — mock OAuth handshake.
3. Browse branches.
4. Browse files.
5. Ver commits.
6. Conectar a pipeline.

CRITERIOS: ≥ 5 tests.
```

## Tarea 16.7 — Insight / Investigator

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/insight/InvestigatorPage.tsx.

OBJETIVO: apps/web/e2e/insight.spec.ts.
CASOS: render, búsqueda, drill-down, ≥ 4 tests.
```

## Tarea 16.8 — Peer Manager

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/peer-manager/PeerManagerPage.tsx.

OBJETIVO: apps/web/e2e/peer-manager.spec.ts.
CASOS: gestión de peers/federación, lista, conectar/desconectar, ≥ 4 tests.
```

## Tarea 16.9 — Cipher

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/cipher/CipherPage.tsx.

OBJETIVO: apps/web/e2e/cipher.spec.ts.
CASOS: gestión de secretos/keys, crear, rotar, eliminar, ≥ 4 tests. Verifica que valores secretos NUNCA se muestran en logs/DOM tras revelarlos.
```

## Tarea 16.10 — Sensitive Data Scanner (SDS)

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/sds/SensitiveDataScannerPage.tsx.

OBJETIVO: apps/web/e2e/sds.spec.ts.

CASOS:
1. Lista de scans.
2. Crear scan: target dataset + categorías PII.
3. Ejecutar.
4. Resultados con findings (drill-down al row offending).
5. Marcar como falso positivo.
6. Configurar reglas custom.

CRITERIOS: ≥ 5 tests.
```

---

# Fase 17 — Settings, Audit/Approvals, Retention

## Tarea 17.1 — Settings

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/settings/SettingsPage.tsx y apps/web/e2e/pages/SettingsPage.ts.

OBJETIVO: apps/web/e2e/settings.spec.ts.

CASOS:
1. Tabs: Profile, Security, Notifications, API tokens, Preferences.
2. Profile: editar nombre, avatar (file input), bio.
3. Security: cambiar password (current + new + confirm).
4. Habilitar MFA.
5. Sesiones activas: revocar sesión.
6. Notifications: toggles por categoría (email, push).
7. API tokens: crear (genera token, asserta visible una sola vez), revocar.
8. Preferences: theme (light/dark/system), idioma.

CRITERIOS: ≥ 7 tests.
```

## Tarea 17.2 — Audit / Approvals

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/audit/AuditPage.tsx.

OBJETIVO: apps/web/e2e/audit.spec.ts.

CASOS:
1. Tab "Audit log": filtros (usuario, acción, recurso, fechas), paginación.
2. Detalle de evento (drawer con before/after JSON).
3. Tab "Approvals": queue de pending approvals.
4. Aprobar / rechazar con comentario.
5. Ver approvals históricas (filtro por estado).
6. Asignar approver delegate.

CRITERIOS: ≥ 5 tests.
```

## Tarea 17.3 — Retention

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/retention/RetentionPoliciesPage.tsx.

OBJETIVO: apps/web/e2e/retention.spec.ts.

CASOS:
1. Lista de políticas.
2. Crear policy: target (dataset/object set) + TTL + acción (archive, delete).
3. Editar.
4. Pausar.
5. Ver ejecuciones pasadas.
6. Eliminar.

CRITERIOS: ≥ 5 tests.
```

---

# Fase 18 — Control Panel

> 17 sub-pantallas. Cada una su propio spec.

## Tarea 18.1 — Control Panel home

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/control-panel/ControlPanelPage.tsx.

OBJETIVO: apps/web/e2e/cp-home.spec.ts.

CASOS:
1. Render dashboard admin con KPIs (usuarios, sesiones activas, storage, etc.).
2. Navegación a cada sub-sección via tiles/sidebar.
3. Verificar gating: con `viewerPage` no se puede acceder (redirect a /).

CRITERIOS: ≥ 3 tests.
```

## Tarea 18.2 — Users

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/control-panel/UsersPage.tsx.

OBJETIVO: apps/web/e2e/cp-users.spec.ts.

CASOS:
1. Lista de usuarios con paginación + búsqueda.
2. Filtro por rol, estado (active, suspended).
3. Invitar usuario: email + rol → POST.
4. Editar usuario: cambiar rol, suspend, force MFA, reset password.
5. Detalle: ver sesiones, permisos efectivos, audit.
6. Bulk: suspend múltiples.
7. Exportar lista (CSV).
8. Asignar a grupos.

CRITERIOS: ≥ 7 tests.
```

## Tarea 18.3 — Groups

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/control-panel/GroupsPage.tsx.

OBJETIVO: apps/web/e2e/cp-groups.spec.ts.

CASOS:
1. Lista de grupos.
2. Crear grupo: nombre, descripción.
3. Añadir miembros (autocomplete usuarios).
4. Quitar miembros.
5. Editar grupo.
6. Asignar permisos al grupo.
7. Eliminar grupo (warning con #miembros afectados).

CRITERIOS: ≥ 6 tests.
```

## Tarea 18.4 — Identity Providers

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/control-panel/IdentityProvidersPage.tsx.

OBJETIVO: apps/web/e2e/cp-identity-providers.spec.ts.

CASOS:
1. Lista de IdPs configurados (OIDC, SAML).
2. Añadir IdP: tipo + config (issuer, client_id, secret, scopes).
3. Test connection.
4. JIT provisioning toggle.
5. Group mappings (regex/claim → group).
6. Enable/disable.
7. Eliminar (con warning).

CRITERIOS: ≥ 6 tests.
```

## Tarea 18.5 — Tenancy

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/control-panel/TenancyPage.tsx.

OBJETIVO: apps/web/e2e/cp-tenancy.spec.ts.

CASOS:
1. Settings de la instancia: nombre, logo, branding.
2. Dominios permitidos.
3. Quotas (storage, compute).
4. Region/residency settings.

CRITERIOS: ≥ 4 tests.
```

## Tarea 18.6 — Role Sets

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/control-panel/RoleSetsPage.tsx.

OBJETIVO: apps/web/e2e/cp-role-sets.spec.ts.

CASOS:
1. Lista de role sets.
2. Crear: nombre + roles (matriz de permisos).
3. Editar matriz.
4. Asignar a usuarios/grupos.
5. Eliminar.

CRITERIOS: ≥ 4 tests.
```

## Tarea 18.7 — Marking Categories

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/control-panel/MarkingCategoriesPage.tsx.

OBJETIVO: apps/web/e2e/cp-marking-categories.spec.ts.

CASOS:
1. Lista de categories (markings tipo PII, Classified, etc.).
2. Crear: nombre, color, markings hijos.
3. Asignar a recursos.
4. Editar/eliminar.

CRITERIOS: ≥ 4 tests.
```

## Tarea 18.8 — Scoped Sessions

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/control-panel/ScopedSessionsPage.tsx.

OBJETIVO: apps/web/e2e/cp-scoped-sessions.spec.ts.

CASOS:
1. Lista de sesiones con scope reducido.
2. Crear sesión: usuario + scope + duración.
3. Revocar.
4. Audit.

CRITERIOS: ≥ 4 tests.
```

## Tarea 18.9 — Application Access

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/control-panel/ApplicationAccessPage.tsx.

OBJETIVO: apps/web/e2e/cp-application-access.spec.ts.

CASOS:
1. Matriz de acceso por aplicación.
2. Conceder/revocar acceso a usuario/grupo por app.
3. Filtros.

CRITERIOS: ≥ 3 tests.
```

## Tarea 18.10 — Third Party Applications

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/control-panel/ThirdPartyApplicationsPage.tsx.

OBJETIVO: apps/web/e2e/cp-third-party-apps.spec.ts.

CASOS:
1. Lista de apps OAuth registradas.
2. Crear OAuth client: nombre, redirect URIs, scopes.
3. Rotar client secret.
4. Revocar.
5. Ver tokens activos.

CRITERIOS: ≥ 4 tests.
```

## Tarea 18.11 — Member Discovery

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/control-panel/MemberDiscoveryPage.tsx.

OBJETIVO: apps/web/e2e/cp-member-discovery.spec.ts.

CASOS:
1. Config de discovery (LDAP, SCIM, directorio).
2. Test sync.
3. Mappings.
4. Manual sync now.

CRITERIOS: ≥ 3 tests.
```

## Tarea 18.12 — File Access Presets

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/control-panel/FileAccessPresetsPage.tsx.

OBJETIVO: apps/web/e2e/cp-file-access-presets.spec.ts.

CASOS:
1. Lista de presets.
2. Crear preset: nombre + matriz de permisos.
3. Asignar como default a proyecto/tipo de archivo.
4. Eliminar.

CRITERIOS: ≥ 3 tests.
```

## Tarea 18.13 — Retention Policies (CP)

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/control-panel/RetentionPoliciesPage.tsx (versión admin).

OBJETIVO: apps/web/e2e/cp-retention.spec.ts.

CASOS: similares a Tarea 17.3 pero alcance global. ≥ 4 tests.
```

## Tarea 18.14 — Streaming Profiles

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/control-panel/StreamingProfilesPage.tsx.

OBJETIVO: apps/web/e2e/cp-streaming-profiles.spec.ts.
CASOS: gestión de profiles (compresion, retention, partitions), ≥ 4 tests.
```

## Tarea 18.15 — Data Health

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/control-panel/DataHealthPage.tsx.

OBJETIVO: apps/web/e2e/cp-data-health.spec.ts.

CASOS:
1. Dashboard con checks (stale datasets, failed builds, etc.).
2. Drill-down a check específico.
3. Acción remediar (si aplica).
4. Configurar thresholds.

CRITERIOS: ≥ 4 tests.
```

## Tarea 18.16 — Projects (admin)

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/control-panel/ProjectsPage.tsx.

OBJETIVO: apps/web/e2e/cp-projects.spec.ts.

CASOS:
1. Vista admin de todos los proyectos.
2. Transferir ownership.
3. Archivar / unarchive.
4. Ver quotas y uso.

CRITERIOS: ≥ 4 tests.
```

## Tarea 18.17 — Restricted Views

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/control-panel/RestrictedViewsPage.tsx.

OBJETIVO: apps/web/e2e/cp-restricted-views.spec.ts.

CASOS:
1. Lista de vistas restringidas.
2. Crear: dataset/object set + condición + roles autorizados.
3. Test con diferentes roles (mock multi-user).
4. Eliminar.

CRITERIOS: ≥ 4 tests.
```

---

# Fase 19 — Apps, OSDK, Custom Endpoints, Widgets, Developers, Pilot, Ontology Indexing

## Tarea 19.1 — Applications

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/applications/ApplicationsPage.tsx.

OBJETIVO: apps/web/e2e/applications.spec.ts.
CASOS: lista de aplicaciones desplegadas, deploy/undeploy, logs, ≥ 4 tests.
```

## Tarea 19.2 — OSDK Apps

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/osdk-apps/OsdkAppsPage.tsx.

OBJETIVO: apps/web/e2e/osdk-apps.spec.ts.
CASOS: gestión de apps OSDK (SDK generados), generar SDK, descargar, ≥ 4 tests.
```

## Tarea 19.3 — Custom Endpoints

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/custom-endpoints/CustomEndpointsPage.tsx.

OBJETIVO: apps/web/e2e/custom-endpoints.spec.ts.

CASOS:
1. Lista de endpoints.
2. Crear: path + method + handler (función) + auth required.
3. Test (form con body + curl preview).
4. Versionado.
5. Eliminar.

CRITERIOS: ≥ 4 tests.
```

## Tarea 19.4 — Checkpoints

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/checkpoints/CheckpointsPage.tsx.

OBJETIVO: apps/web/e2e/checkpoints.spec.ts.
CASOS: crear, restaurar, eliminar checkpoints, ≥ 4 tests.
```

## Tarea 19.5 — Custom Widgets

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/widgets/CustomWidgetsPage.tsx.

OBJETIVO: apps/web/e2e/custom-widgets.spec.ts.

CASOS:
1. Lista de widgets custom.
2. Crear widget: nombre, código (Monaco), schema de props, preview.
3. Publicar para uso en Workshop.
4. Versionado.
5. Eliminar.

CRITERIOS: ≥ 4 tests.
```

## Tarea 19.6 — Developers

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/developers/DevelopersPage.tsx.

OBJETIVO: apps/web/e2e/developers.spec.ts.
CASOS: hub de developer tools, API docs, SDK downloads, links a docs externas, ≥ 4 tests.
```

## Tarea 19.7 — Pilot

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/pilot/PilotPage.tsx.

OBJETIVO: apps/web/e2e/pilot.spec.ts.
CASOS: identificar qué es Pilot leyendo el código y cubrir flujo principal con ≥ 4 tests.
```

## Tarea 19.8 — Ontology Indexing

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/ontology-indexing/OntologyIndexingPage.tsx.

OBJETIVO: apps/web/e2e/ontology-indexing.spec.ts.

CASOS:
1. Estado de indices.
2. Reindex on demand.
3. Configurar índices (campos a indexar).
4. Ver progreso.

CRITERIOS: ≥ 4 tests.
```

---

# Fase 20 — Demos, 404 y redirects legacy

## Tarea 20.1 — Demos (Charts, Monaco, MapLibre, Cytoscape)

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/charts-demo/, monaco-demo/, maplibre-demo/, cytoscape-demo/.

OBJETIVO: apps/web/e2e/demos.spec.ts (un spec con tests para los 4).

CASOS:
1. Charts demo: render de gráficos, interacciones básicas.
2. Monaco demo: editor visible, tipear texto, autocomplete (mock).
3. MapLibre demo: mapa carga, controles, basemap switch.
4. Cytoscape demo: grafo carga, layout switch, nodos clickables.
5. Cada demo no debe romper en mobile viewport (test responsive).

CRITERIOS: ≥ 5 tests.
```

## Tarea 20.2 — 404 NotFound + redirects legacy

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/src/routes/NotFound.tsx y apps/web/src/router.tsx para encontrar los redirects legacy.

OBJETIVO: apps/web/e2e/not-found-and-redirects.spec.ts.

CASOS:
1. URL aleatoria inexistente → renderiza NotFound con CTA "Go home".
2. NotFound respeta autenticación (con AppShell para users autenticados, sin para no auth).
3. Cada redirect legacy navega al destino correcto:
   - `/workflows` → `/foundry-rules`
   - `/automate` → `/foundry-rules`
   - `/nexus` → `/ai/threads`
   - `/functions` → `/compute-modules`
   - `/ontology-design` → `/ontology-manager`
   - `/ontologies` → `/ontology-manager`
   - `/audit` → `/approvals`
   - `/reports` → `/slate`
   - `/ml` → `/model-catalog`
   - `/dashboards` → `/quiver`
   - `/dashboards/:id` → `/quiver`
   - `/queries` → `/object-explorer`
   - `/marketplace`, `/marketplace/:id`, `/global-branching`, `/workflow-lineage` → comprobar destinos en router.tsx.
4. Redirect preserva query params si aplica.

CRITERIOS: 1 test por redirect + 2 generales = ≥ 13 tests programáticos (loop sobre lista).
```

---

# Fase 21 — Cross-cutting

## Tarea 21.1 — Auditoría de accesibilidad sistemática

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/e2e/helpers/a11y.ts.

OBJETIVO: apps/web/e2e/a11y-all-routes.spec.ts.

CASOS:
1. Para cada ruta definida en router.tsx, ejecuta `auditPageA11y(page, { route })` con `authedPage`.
2. Genera lista de violaciones por ruta (no falla por warnings — solo critical/serious).
3. Output en `test-results/a11y/summary.json` con count por regla y ruta.
4. Tests fallan solo si `critical` o `serious` count > 0 (configurable per ruta via allowlist).

REQUISITOS:
- Mockea con `installDefaultApiMocks` para que las páginas carguen.
- Excluye widgets de terceros conocidos (Monaco, Cytoscape, MapLibre, ECharts) en cada audit.
- Programa los tests con `for (const route of ROUTES) test(route.path, ...)`.

CRITERIOS:
- Spec corre en CI con tag `@a11y` para poder ejecutarse separadamente.
- Documenta en apps/web/e2e/README.md cómo aceptar nuevas violaciones (allowlist con justificación).
```

## Tarea 21.2 — Regresión visual sistemática

```
Eres Claude Code en /home/user/OpenFoundry. Lee apps/web/e2e/helpers/visual.ts.

OBJETIVO: apps/web/e2e/visual-all-routes.spec.ts.

CASOS:
1. Para cada ruta, navega + `prepareForVisual(page)` + `expectScreenshot(page, route.name)`.
2. Genera baselines en `apps/web/e2e/__snapshots__/visual-all-routes/`.
3. Solo Chromium (firefox/webkit no son deterministas para visual).
4. Máscaras estándar: timestamps, IDs (`[data-testid$="-id"]`), avatares.

REQUISITOS:
- Tag `@visual`.
- Baselines deben commitearse al repo.
- Documenta proceso de actualización en README (cuando un cambio UI es intencional).
- Mobile viewport: spec adicional `visual-mobile.spec.ts` con `viewport: { width: 375, height: 667 }`.

CRITERIOS:
- Primera ejecución genera baselines (--update-snapshots), segunda ejecución pasa con 0 diff.
```

## Tarea 21.3 — Responsiveness sistemática

```
Eres Claude Code en /home/user/OpenFoundry.

OBJETIVO: apps/web/e2e/responsive.spec.ts.

CASOS:
1. Para cada breakpoint definido (mobile 375×667, tablet 768×1024, desktop 1440×900), verifica en al menos las 20 rutas más importantes:
   - AppShell collapse en mobile (sidebar oculto, hamburger button).
   - Tablas pasan a card view o scroll horizontal sin desbordar.
   - Modales centrados y caben.
   - Sin scrollbars horizontales no intencionales.
2. Mide CLS (Cumulative Layout Shift) con `page.evaluate` sobre `PerformanceObserver` y asserta < 0.1.

CRITERIOS:
- ≥ 60 tests (20 rutas × 3 breakpoints).
- Tag `@responsive`.
```

## Tarea 21.4 — Tests de permisos / RBAC

```
Eres Claude Code en /home/user/OpenFoundry.

OBJETIVO: apps/web/e2e/permissions.spec.ts.

CASOS:
1. Define matrix de roles: admin, editor, viewer, guest.
2. Para cada ruta sensible (control-panel/*, audit, pipelines, datasets):
   - admin: ve y puede modificar.
   - editor: ve, puede modificar lo suyo.
   - viewer: ve solo lectura, botones "Create/Edit/Delete" ausentes.
   - guest: redirect a /auth/login.
3. Verifica que API calls bloqueadas por backend muestran el mensaje correcto en UI (toast/banner).

REQUISITOS:
- Usar `buildUser({ roles: [...], permissions: [...] })` para cada fixture.
- ≥ 30 tests (4 roles × 8 rutas sensibles aproximadamente).
```

## Tarea 21.5 — Tests de error boundaries y network failures

```
Eres Claude Code en /home/user/OpenFoundry.

OBJETIVO: apps/web/e2e/error-handling.spec.ts.

CASOS:
1. Para cada área principal: mock al endpoint principal con 500 → asserta UI muestra ErrorBoundary con "Something went wrong" + retry.
2. Mock con 503: asserta retry automático con backoff (max 3).
3. Mock con network offline: `page.context().setOffline(true)` → banner "You are offline".
4. Recuperación: tras volver online, el contenido se recarga.
5. Mock con timeout (delay > navigationTimeout): muestra timeout error.
6. JS error en componente: `page.evaluate(() => { throw new Error('x') })` → ErrorBoundary se activa, resto de la app sigue funcionando.

CRITERIOS:
- ≥ 15 tests.
- Verifica que NUNCA queda white screen sin mensaje.
```

## Tarea 21.6 — Internacionalización (si aplica)

```
Eres Claude Code en /home/user/OpenFoundry. Comprueba si apps/web/ usa i18n (busca `useTranslation`, `i18next`, `react-intl`). Si NO existe, marca esta tarea como N/A y reporta.

OBJETIVO (si aplica): apps/web/e2e/i18n.spec.ts.

CASOS:
1. Para cada locale soportado: cambiar idioma en Settings, verificar que strings clave en home están traducidos.
2. Verifica que fechas/números se formatean según locale.
3. RTL si aplica (árabe/hebreo): layout se invierte.

CRITERIOS: ≥ 3 tests por locale, o reporte de N/A.
```

## Tarea 21.7 — Smoke de performance (Core Web Vitals)

```
Eres Claude Code en /home/user/OpenFoundry.

OBJETIVO: apps/web/e2e/perf-smoke.spec.ts.

CASOS:
1. Para 10 rutas críticas (Home, Search, Datasets list, Projects list, etc.):
   - Mide LCP (Largest Contentful Paint) via `PerformanceObserver` → asserta < 2500ms con mocks instalados.
   - Mide FCP (First Contentful Paint) → asserta < 1800ms.
   - Mide TBT (Total Blocking Time) → asserta < 200ms.
2. Reporta resultados en `test-results/perf/<route>.json`.

CRITERIOS:
- Tag `@perf`.
- Tests solo corren en chromium (más estable).
- Documentar baselines en apps/web/e2e/README.md.
```

## Tarea 21.8 — Keyboard navigation

```
Eres Claude Code en /home/user/OpenFoundry.

OBJETIVO: apps/web/e2e/keyboard-nav.spec.ts.

CASOS:
1. Tab navigation desde top de cada página crítica: orden lógico, sin trampas de foco.
2. Atajos globales: Cmd+K (search), Cmd+/ (help), Esc (cierra modal), Cmd+S (save donde aplique).
3. Modales: trap foco dentro, Esc cierra, focus restaurado al trigger.
4. Skip-to-main-content link visible al primer Tab.
5. Form submission con Enter.

CRITERIOS: ≥ 10 tests.
```

---

# Fase 22 — CI hardening y dashboard de cobertura

## Tarea 22.1 — Reporter de cobertura E2E personalizado

```
Eres Claude Code en /home/user/OpenFoundry.

OBJETIVO: Crear apps/web/e2e/reporters/coverage-reporter.ts (Playwright reporter custom) que:
- Lee router.tsx y extrae todas las rutas.
- Cruza con los `page.goto()` y `page.url()` observados durante los tests.
- Genera `test-results/coverage/routes.json` con `{ totalRoutes, coveredRoutes, uncoveredRoutes, percentByArea }`.
- Genera HTML `test-results/coverage/index.html` consumible.

PASOS:
1. Implementa la clase reporter.
2. Registra en playwright.config.ts en el array de reporters.
3. Genera output en CI como artifact.

CRITERIOS:
- Tras correr `playwright test`, genera el report.
- README documenta cómo interpretarlo.
```

## Tarea 22.2 — Job de CI dedicado para cross-cutting

```
Eres Claude Code en /home/user/OpenFoundry. Lee .github/workflows/ci-frontend.yml.

OBJETIVO: Añadir jobs separados para tags `@a11y`, `@visual`, `@perf`, `@responsive`.

CAMBIOS:
1. Job `e2e-a11y`: `playwright test --grep "@a11y"` solo chromium, no falla el build (solo report).
2. Job `e2e-visual`: solo chromium, fallar si diff > threshold.
3. Job `e2e-perf`: solo chromium, smoke perf, no falla (warning).
4. Job `e2e-responsive`: chromium + webkit (incluye iOS).
5. Subir artifacts (a11y JSON, screenshots diff, perf metrics).
6. Comentario en PR con resumen (usar GitHub MCP `add_issue_comment` desde script o action).

CRITERIOS:
- Workflow pasa con configuración correcta.
- PR comentario funcional.
```

## Tarea 22.3 — Detección de flakes

```
Eres Claude Code en /home/user/OpenFoundry.

OBJETIVO: Configurar tracking de flakes:
1. En CI, además de retries por test (ya configurado), ejecutar weekly un job `flake-detector` que corre cada spec 10 veces y reporta tests con failure rate entre 1-99%.
2. Output: `test-results/flakes/report.md`.
3. Auto-create issue en GitHub con lista de flakes detectados.

PASOS:
1. Script en apps/web/e2e/scripts/detect-flakes.ts.
2. Workflow `.github/workflows/flake-detector.yml` con cron weekly.
3. Comment en issues abiertas con tests flaky usando GitHub MCP.

CRITERIOS:
- Workflow corre manualmente con `workflow_dispatch`.
- Genera issue de ejemplo (puedes mockear datos para validar).
```

## Tarea 22.4 — Documentación de testing E2E

```
Eres Claude Code en /home/user/OpenFoundry.

OBJETIVO: Escribir/expandir apps/web/e2e/README.md con:
1. Cómo correr tests localmente (browser único, todos, headed, debug).
2. Convención de naming.
3. Cómo crear nuevos specs (link a scaffold script).
4. Patrones de mocking (auth, REST, SSE, multipart upload).
5. Page Objects: cuándo crear uno, cómo nombrar.
6. Selectores: jerarquía recomendada.
7. Manejo de elementos volátiles (timestamps, IDs random).
8. Snapshots visuales: cuándo regenerar.
9. A11y: cómo allowlistear violations conocidas.
10. CI: tags, shards, browsers.
11. Tabla de cobertura actual con porcentaje por área.

CRITERIOS:
- README ≥ 500 líneas, ejemplos copy-paste.
- Sección "FAQ" con problemas comunes (timeouts, flakes, mock no aplicado).
```

## Tarea 22.5 — Generación automática del informe de cobertura en README

```
Eres Claude Code en /home/user/OpenFoundry.

OBJETIVO: Crear script `apps/web/e2e/scripts/update-coverage-readme.ts` que:
1. Lee `test-results/coverage/routes.json` (output de Tarea 22.1).
2. Actualiza una sección marcada en apps/web/e2e/README.md (entre `<!-- COVERAGE:START -->` y `<!-- COVERAGE:END -->`) con tabla por área + porcentajes.
3. Workflow `.github/workflows/coverage-report.yml` corre tras CI exitoso en main y abre PR con la actualización si cambió.

CRITERIOS:
- Funciona en local: `pnpm --filter @open-foundry/web exec tsx e2e/scripts/update-coverage-readme.ts`.
- PR-auto se abre solo si hay diff.
```

---

# Resumen de tareas

| Fase | # Tareas | # Tests aprox |
|---|---|---|
| 0 — Infra | 5 | 0 (foundation) |
| 1 — Auth | 5 | ~35 |
| 2 — Workspace core | 5 | ~32 |
| 3 — Projects | 3 | ~24 |
| 4 — Datasets | 5 | ~35 |
| 5 — Tablas/objetos | 7 | ~46 |
| 6 — Pipelines/Schedules/Builds | 8 | ~52 |
| 7 — Ontology | 6 | ~30 |
| 8 — Apps/Workshop ampliado | 3 | ~24 |
| 9 — Data Connection | 5 | ~27 |
| 10 — AI | 7 | ~37 |
| 11 — Functions/Actions | 3 | ~17 |
| 12 — Lineage/Visualización | 5 | ~28 |
| 13 — Notebooks/Notepad/Slate | 3 | ~22 |
| 14 — Streaming/Media | 2 | ~14 |
| 15 — Model Catalog/Evals | 2 | ~10 |
| 16 — Avanzada | 10 | ~42 |
| 17 — Settings/Audit/Retention | 3 | ~17 |
| 18 — Control Panel | 17 | ~77 |
| 19 — Apps/OSDK/Custom/Widgets/Devs/Pilot | 8 | ~33 |
| 20 — Demos/404/Redirects | 2 | ~18 |
| 21 — Cross-cutting | 8 | ~100+ |
| 22 — CI hardening | 5 | (infra/CI) |
| **TOTAL** | **~120 tareas** | **~720 tests E2E** |

**Estimación de esfuerzo (asumiendo 1 ingeniero, sin Claude Code):** ~3-4 meses calendario.

**Con Claude Code en paralelo (varias sesiones simultáneas en worktrees):** ~3-5 semanas, dado que las fases 4-19 son altamente paralelizables tras completar la Fase 0.

**Ruta crítica recomendada:**
1. Semana 1: Fase 0 completa (infraestructura).
2. Semana 2-4: Fases 1-20 en paralelo (5-8 tareas concurrentes en worktrees).
3. Semana 5: Fase 21 (cross-cutting).
4. Semana 6: Fase 22 (CI hardening) + estabilización.

---

## Tracking sugerido

Crea un GitHub Project board con:
- **Columna "Backlog"**: todas las tareas.
- **Columna "In progress"**.
- **Columna "Review"** (PR abierto).
- **Columna "Done"**: PR mergeado + Playwright reports verifican que la cobertura % aumentó.

Cada tarea = 1 PR. Tamaño objetivo: ≤ 800 líneas diff (un spec + actualizaciones de Page Objects + fixtures si aplica).
