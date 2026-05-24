import { createBrowserRouter, redirect } from 'react-router-dom';

// Compatibility redirect for routes renamed during the launcher alignment
// to Foundry's canonical app catalog (see docs/reference/launcher-app-mapping.md).
function redirectTo(newPath: string) {
  return ({ request }: { request: Request }) => {
    const url = new URL(request.url);
    return redirect(`${newPath}${url.search}${url.hash}`);
  };
}

import { AppShell } from '@components/AppShell';
import { AuthLayout } from '@components/AuthLayout';
import { Home } from './routes/Home';
import { NotFound } from './routes/NotFound';

export const router = createBrowserRouter([
  {
    path: '/apps/runtime/:slug',
    lazy: async () => ({ Component: (await import('./routes/apps/AppRuntimePage')).AppRuntimePage }),
  },
  {
    path: '/auth',
    element: <AuthLayout />,
    children: [
      {
        path: 'login',
        lazy: async () => ({ Component: (await import('./routes/auth/LoginPage')).LoginPage }),
      },
      {
        path: 'register',
        lazy: async () => ({ Component: (await import('./routes/auth/RegisterPage')).RegisterPage }),
      },
      {
        path: 'setup',
        lazy: async () => ({ Component: (await import('./routes/auth/SetupPage')).SetupPage }),
      },
      {
        path: 'mfa',
        lazy: async () => ({ Component: (await import('./routes/auth/MfaPage')).MfaPage }),
      },
      {
        path: 'callback',
        lazy: async () => ({ Component: (await import('./routes/auth/CallbackPage')).CallbackPage }),
      },
    ],
  },
  {
    path: '/',
    element: <AppShell />,
    errorElement: <NotFound />,
    children: [
      { index: true, element: <Home /> },
      // Legacy URL used by Phase 1 launcher tiles; the canonical routes
      // below render the same component but at stable per-app paths.
      {
        path: 'coming-soon',
        lazy: async () => ({ Component: (await import('./routes/AppRoadmapPage')).AppRoadmapPage }),
      },
      // Canonical landings for Foundry-parity apps that have not yet been
      // built out. Once a dedicated page exists for an app, replace its
      // entry here with a normal lazy import.
      {
        path: 'pipelines/linter',
        lazy: async () => ({ Component: (await import('./routes/pipeline-linter/LinterPage')).LinterPage }),
      },
      {
        path: 'peer-manager',
        lazy: async () => ({ Component: (await import('./routes/peer-manager/PeerManagerPage')).PeerManagerPage }),
      },
      {
        path: 'insight',
        lazy: async () => ({ Component: (await import('./routes/insight/InvestigatorPage')).InvestigatorPage }),
      },
      {
        path: 'ai/assist',
        lazy: async () => ({ Component: (await import('./routes/ai/AssistPage')).AssistPage }),
      },
      {
        path: 'ai/analyst',
        lazy: async () => ({ Component: (await import('./routes/ai/AnalystPage')).AnalystPage }),
      },
      {
        path: 'ai/threads',
        lazy: async () => ({ Component: (await import('./routes/ai/ThreadsPage')).ThreadsPage }),
      },
      {
        path: 'ai/documents',
        lazy: async () => ({ Component: (await import('./routes/ai/DocumentsPage')).DocumentsPage }),
      },
      {
        path: 'ai/chatbot-studio',
        lazy: async () => ({ Component: (await import('./routes/ai/ChatbotStudioPage')).ChatbotStudioPage }),
      },
      {
        path: 'ai/operator',
        lazy: async () => ({ Component: (await import('./routes/ai/OperatorPage')).OperatorPage }),
      },
      {
        path: 'slate',
        lazy: async () => ({ Component: (await import('./routes/slate/SlatePage')).SlatePage }),
      },
      {
        path: 'pilot',
        lazy: async () => ({ Component: (await import('./routes/pilot/PilotPage')).PilotPage }),
      },
      {
        path: 'widgets',
        lazy: async () => ({ Component: (await import('./routes/widgets/CustomWidgetsPage')).CustomWidgetsPage }),
      },
      {
        path: 'osdk-apps',
        lazy: async () => ({ Component: (await import('./routes/osdk-apps/OsdkAppsPage')).OsdkAppsPage }),
      },
      {
        path: 'custom-endpoints',
        lazy: async () => ({ Component: (await import('./routes/custom-endpoints/CustomEndpointsPage')).CustomEndpointsPage }),
      },
      {
        path: 'checkpoints',
        lazy: async () => ({ Component: (await import('./routes/checkpoints/CheckpointsPage')).CheckpointsPage }),
      },
      {
        path: 'cipher',
        lazy: async () => ({ Component: (await import('./routes/cipher/CipherPage')).CipherPage }),
      },
      {
        path: 'sds',
        lazy: async () => ({ Component: (await import('./routes/sds/SensitiveDataScannerPage')).SensitiveDataScannerPage }),
      },
      {
        path: 'retention',
        lazy: async () => ({ Component: (await import('./routes/retention/RetentionPoliciesPage')).RetentionPoliciesPage }),
      },
      {
        path: 'applications',
        lazy: async () => ({ Component: (await import('./routes/applications/ApplicationsPage')).ApplicationsPage }),
      },
      {
        path: 'notifications',
        lazy: async () => ({ Component: (await import('./routes/notifications/NotificationsPage')).NotificationsPage }),
      },
      {
        path: 'recent',
        lazy: async () => ({ Component: (await import('./routes/recent/RecentPage')).RecentPage }),
      },
      {
        path: 'favorites',
        lazy: async () => ({ Component: (await import('./routes/favorites/FavoritesPage')).FavoritesPage }),
      },
      {
        path: 'settings',
        lazy: async () => ({ Component: (await import('./routes/settings/SettingsPage')).SettingsPage }),
      },
      // Removed from the canonical launcher catalog (Phase 5 cleanup).
      // Functionality is covered by Quiver, which provides drill-down
      // dashboards on top of object and time series data.
      { path: 'dashboards', loader: redirectTo('/quiver') },
      { path: 'dashboards/:id', loader: redirectTo('/quiver') },
      // Workflow lineage is covered by Data Lineage.
      { path: 'workflow-lineage', loader: redirectTo('/lineage') },
      {
        path: 'lineage',
        lazy: async () => ({ Component: (await import('./routes/lineage/LineagePage')).LineagePage }),
      },
      {
        path: 'notebooks',
        lazy: async () => ({ Component: (await import('./routes/notebooks/NotebooksListPage')).NotebooksListPage }),
      },
      {
        path: 'notebooks/:id',
        lazy: async () => ({ Component: (await import('./routes/notebooks/NotebookDetailPage')).NotebookDetailPage }),
      },
      {
        path: 'contour',
        lazy: async () => ({ Component: (await import('./routes/contour/ContourPage')).ContourPage }),
      },
      {
        path: 'geospatial',
        lazy: async () => ({ Component: (await import('./routes/geospatial/GeospatialPage')).GeospatialPage }),
      },
      {
        path: 'search',
        lazy: async () => ({ Component: (await import('./routes/search/SearchPage')).SearchPage }),
      },
      // Search-style queries are surfaced from Object Explorer in the
      // canonical catalog.
      { path: 'queries', loader: redirectTo('/object-explorer') },
      {
        path: 'quiver',
        lazy: async () => ({ Component: (await import('./routes/quiver/QuiverPage')).QuiverPage }),
      },
      {
        path: 'vertex',
        lazy: async () => ({ Component: (await import('./routes/vertex/VertexPage')).VertexPage }),
      },
      {
        path: 'notepad',
        lazy: async () => ({ Component: (await import('./routes/notepad/NotepadListPage')).NotepadListPage }),
      },
      {
        path: 'notepad/:id',
        lazy: async () => ({ Component: (await import('./routes/notepad/NotepadDetailPage')).NotepadDetailPage }),
      },
      // Narrative reports merge into Slate's Web App Studio.
      { path: 'reports', loader: redirectTo('/slate') },
      // No canonical equivalent for Global Branching; send to Workspace.
      { path: 'global-branching', loader: redirectTo('/') },
      {
        path: 'developers',
        lazy: async () => ({ Component: (await import('./routes/developers/DevelopersPage')).DevelopersPage }),
      },
      {
        path: 'object-databases',
        lazy: async () => ({ Component: (await import('./routes/object-databases/ObjectDatabasesPage')).ObjectDatabasesPage }),
      },
      // Workflow orchestration folds into Operational Rules (Foundry Rules).
      { path: 'workflows', loader: redirectTo('/foundry-rules') },
      // Ontology design is part of Ontology Manager in the canonical catalog.
      { path: 'ontology-design', loader: redirectTo('/ontology-manager') },
      {
        path: 'dynamic-scheduling',
        lazy: async () => ({ Component: (await import('./routes/dynamic-scheduling/DynamicSchedulingPage')).DynamicSchedulingPage }),
      },
      {
        path: 'interfaces',
        lazy: async () => ({ Component: (await import('./routes/interfaces/InterfacesPage')).InterfacesPage }),
      },
      {
        path: 'build-schedules',
        lazy: async () => ({ Component: (await import('./routes/build-schedules/BuildSchedulesPage')).BuildSchedulesPage }),
      },
      {
        path: 'build-schedules/sweep',
        lazy: async () => ({ Component: (await import('./routes/build-schedules/SweepPage')).SweepPage }),
      },
      {
        path: 'fusion',
        lazy: async () => ({ Component: (await import('./routes/fusion/FusionPage')).FusionPage }),
      },
      // Grounded LLM agents land in the canonical AI Threads experience.
      { path: 'nexus', loader: redirectTo('/ai/threads') },
      {
        path: 'approvals',
        lazy: async () => ({ Component: (await import('./routes/approvals/ApprovalsPage')).ApprovalsPage }),
      },
      {
        path: 'audit',
        lazy: async () => ({ Component: (await import('./routes/audit/AuditPage')).AuditPage }),
      },
      {
        path: 'code-repos',
        lazy: async () => ({ Component: (await import('./routes/code-repos/CodeReposPage')).CodeReposPage }),
      },
      // No canonical equivalent for Marketplace; send to Workspace.
      { path: 'marketplace', loader: redirectTo('/') },
      { path: 'marketplace/:id', loader: redirectTo('/') },
      {
        path: 'virtual-tables',
        lazy: async () => ({ Component: (await import('./routes/virtual-tables/VirtualTablesPage')).VirtualTablesPage }),
      },
      {
        path: 'virtual-tables/:rid',
        lazy: async () => ({ Component: (await import('./routes/virtual-tables/VirtualTableDetailPage')).VirtualTableDetailPage }),
      },
      {
        path: 'ai',
        lazy: async () => ({ Component: (await import('./routes/ai/AiPage')).AiPage }),
      },
      {
        path: 'logic',
        lazy: async () => ({ Component: (await import('./routes/logic/LogicAuthoringPage')).LogicAuthoringPage }),
      },
      // Automation surface maps to Operational Rules.
      { path: 'automate', loader: redirectTo('/foundry-rules') },
      {
        path: 'aip-evals',
        lazy: async () => ({ Component: (await import('./routes/aip-evals/AipEvalsPage')).AipEvalsPage }),
      },
      {
        path: 'object-views',
        lazy: async () => ({ Component: (await import('./routes/object-views/ObjectViewsPage')).ObjectViewsPage }),
      },
      {
        path: 'object-explorer',
        lazy: async () => ({ Component: (await import('./routes/object-explorer/ObjectExplorerPage')).ObjectExplorerPage }),
      },
      {
        path: 'iceberg-tables',
        lazy: async () => ({ Component: (await import('./routes/iceberg-tables/IcebergTablesPage')).IcebergTablesPage }),
      },
      {
        path: 'iceberg-tables/:id',
        lazy: async () => ({ Component: (await import('./routes/iceberg-tables/IcebergTableDetailPage')).IcebergTableDetailPage }),
      },
      {
        path: 'ontology-indexing',
        lazy: async () => ({ Component: (await import('./routes/ontology-indexing/OntologyIndexingPage')).OntologyIndexingPage }),
      },
      // Ontology project browser is part of Ontology Manager.
      { path: 'ontologies', loader: redirectTo('/ontology-manager') },
      {
        path: 'object-monitors',
        lazy: async () => ({ Component: (await import('./routes/object-monitors/ObjectMonitorsPage')).ObjectMonitorsPage }),
      },
      {
        path: 'streaming',
        lazy: async () => ({ Component: (await import('./routes/streaming/StreamingPage')).StreamingPage }),
      },
      {
        path: 'streaming/:id',
        lazy: async () => ({ Component: (await import('./routes/streaming/StreamingDetailPage')).StreamingDetailPage }),
      },
      {
        path: 'machinery',
        lazy: async () => ({ Component: (await import('./routes/machinery/MachineryPage')).MachineryPage }),
      },
      {
        path: 'media-sets',
        lazy: async () => ({ Component: (await import('./routes/media-sets/MediaSetsPage')).MediaSetsPage }),
      },
      {
        path: 'media-sets/:rid',
        lazy: async () => ({ Component: (await import('./routes/media-sets/MediaSetDetailPage')).MediaSetDetailPage }),
      },
      {
        path: 'object-link-types',
        lazy: async () => ({ Component: (await import('./routes/object-link-types/ObjectLinkTypesPage')).ObjectLinkTypesPage }),
      },
      {
        path: 'object-link-types/:id',
        lazy: async () => ({ Component: (await import('./routes/link-types/LinkTypeDetailPage')).LinkTypeDetailPage }),
      },
      {
        path: 'builds',
        lazy: async () => ({ Component: (await import('./routes/builds/BuildsPage')).BuildsPage }),
      },
      {
        path: 'builds/:rid',
        lazy: async () => ({ Component: (await import('./routes/builds/BuildDetailPage')).BuildDetailPage }),
      },
      {
        path: 'foundry-rules',
        lazy: async () => ({ Component: (await import('./routes/foundry-rules/FoundryRulesPage')).FoundryRulesPage }),
      },
      {
        path: 'control-panel',
        lazy: async () => ({ Component: (await import('./routes/control-panel/ControlPanelPage')).ControlPanelPage }),
      },
      {
        path: 'control-panel/development-portal',
        lazy: async () => ({
          Component: (await import('./routes/control-panel/DevelopmentPortalPage')).DevelopmentPortalPage,
        }),
      },
      {
        path: 'control-panel/streaming-profiles',
        lazy: async () => ({ Component: (await import('./routes/control-panel/StreamingProfilesPage')).StreamingProfilesPage }),
      },
      {
        path: 'control-panel/data-health',
        lazy: async () => ({ Component: (await import('./routes/control-panel/DataHealthPage')).DataHealthPage }),
      },
      {
        path: 'control-panel/tenancy',
        lazy: async () => ({ Component: (await import('./routes/control-panel/TenancyPage')).TenancyPage }),
      },
      {
        path: 'control-panel/identity-providers',
        lazy: async () => ({ Component: (await import('./routes/control-panel/IdentityProvidersPage')).IdentityProvidersPage }),
      },
      {
        path: 'control-panel/users',
        lazy: async () => ({ Component: (await import('./routes/control-panel/UsersPage')).UsersPage }),
      },
      {
        path: 'control-panel/groups',
        lazy: async () => ({ Component: (await import('./routes/control-panel/GroupsPage')).GroupsPage }),
      },
      {
        path: 'control-panel/projects',
        lazy: async () => ({ Component: (await import('./routes/control-panel/ProjectsPage')).ProjectsPage }),
      },
      {
        path: 'control-panel/role-sets',
        lazy: async () => ({ Component: (await import('./routes/control-panel/RoleSetsPage')).RoleSetsPage }),
      },
      {
        path: 'control-panel/marking-categories',
        lazy: async () => ({ Component: (await import('./routes/control-panel/MarkingCategoriesPage')).MarkingCategoriesPage }),
      },
      {
        path: 'control-panel/scoped-sessions',
        lazy: async () => ({ Component: (await import('./routes/control-panel/ScopedSessionsPage')).ScopedSessionsPage }),
      },
      {
        path: 'control-panel/application-access',
        lazy: async () => ({ Component: (await import('./routes/control-panel/ApplicationAccessPage')).ApplicationAccessPage }),
      },
      {
        path: 'control-panel/third-party-applications',
        lazy: async () => ({ Component: (await import('./routes/control-panel/ThirdPartyApplicationsPage')).ThirdPartyApplicationsPage }),
      },
      {
        path: 'control-panel/member-discovery',
        lazy: async () => ({ Component: (await import('./routes/control-panel/MemberDiscoveryPage')).MemberDiscoveryPage }),
      },
      {
        path: 'control-panel/file-access-presets',
        lazy: async () => ({ Component: (await import('./routes/control-panel/FileAccessPresetsPage')).FileAccessPresetsPage }),
      },
      {
        path: 'control-panel/retention-policies',
        lazy: async () => ({ Component: (await import('./routes/control-panel/RetentionPoliciesPage')).RetentionPoliciesPage }),
      },
      {
        path: 'control-panel/restricted-views',
        lazy: async () => ({ Component: (await import('./routes/control-panel/RestrictedViewsPage')).RestrictedViewsPage }),
      },
      {
        path: 'compute-modules',
        lazy: async () => ({ Component: (await import('./routes/functions/FunctionsPage')).FunctionsPage }),
      },
      {
        path: 'compute-modules/:id',
        lazy: async () => ({ Component: (await import('./routes/functions/FunctionDetailPage')).FunctionDetailPage }),
      },
      {
        path: 'dev/components',
        lazy: async () => ({ Component: (await import('./routes/dev/ComponentsGalleryPage')).ComponentsGalleryPage }),
      },
      {
        path: 'functions',
        loader: redirectTo('/compute-modules'),
      },
      {
        path: 'pipelines',
        lazy: async () => ({ Component: (await import('./routes/pipelines/PipelinesPage')).PipelinesPage }),
      },
      {
        path: 'pipelines/new',
        lazy: async () => ({ Component: (await import('./routes/pipelines/PipelineNewPage')).PipelineNewPage }),
      },
      {
        path: 'pipelines/:id/edit',
        lazy: async () => ({ Component: (await import('./routes/pipelines/PipelineEditPage')).PipelineEditPage }),
      },
      {
        path: 'pipelines/:id/runs/:runId',
        lazy: async () => ({ Component: (await import('./routes/pipelines/PipelineEditPage')).PipelineEditPage }),
      },
      {
        path: 'schedules/new',
        lazy: async () => ({ Component: (await import('./routes/schedules/NewSchedulePage')).NewSchedulePage }),
      },
      {
        path: 'schedules/:rid',
        lazy: async () => ({ Component: (await import('./routes/schedules/ScheduleDetailPage')).ScheduleDetailPage }),
      },
      {
        path: 'model-catalog',
        lazy: async () => ({ Component: (await import('./routes/ml/MlPage')).MlPage }),
      },
      {
        path: 'llm-catalog',
        lazy: async () => ({ Component: (await import('./routes/llm-catalog/LlmCatalogPage')).LlmCatalogPage }),
      },
      {
        path: 'ml',
        loader: redirectTo('/model-catalog'),
      },
      {
        path: 'action-types',
        lazy: async () => ({ Component: (await import('./routes/action-types/ActionTypesPage')).ActionTypesPage }),
      },
      {
        path: 'action-types/:id',
        lazy: async () => ({ Component: (await import('./routes/action-types/ActionTypeDetailPage')).ActionTypeDetailPage }),
      },
      {
        path: 'datasets',
        lazy: async () => ({ Component: (await import('./routes/datasets/DatasetsListPage')).DatasetsListPage }),
      },
      {
        path: 'datasets/upload',
        lazy: async () => ({ Component: (await import('./routes/datasets/DatasetUploadPage')).DatasetUploadPage }),
      },
      {
        path: 'datasets/:id',
        lazy: async () => ({ Component: (await import('./routes/datasets/DatasetDetailPage')).DatasetDetailPage }),
      },
      {
        path: 'datasets/:id/branches',
        lazy: async () => ({ Component: (await import('./routes/datasets/DatasetBranchesPage')).DatasetBranchesPage }),
      },
      {
        path: 'datasets/:id/branches/:branch',
        lazy: async () => ({ Component: (await import('./routes/datasets/DatasetBranchDetailPage')).DatasetBranchDetailPage }),
      },
      {
        path: 'apps',
        lazy: async () => ({ Component: (await import('./routes/apps/AppsPage')).AppsPage }),
      },
      {
        path: 'apps/:id/workshop',
        lazy: async () => ({ Component: (await import('./routes/apps/WorkshopEditorPage')).WorkshopEditorPage }),
      },
      {
        path: 'data-connection',
        lazy: async () => ({ Component: (await import('./routes/data-connection/DataConnectionPage')).DataConnectionPage }),
      },
      {
        path: 'data-connection/agents',
        lazy: async () => ({ Component: (await import('./routes/data-connection/AgentsPage')).AgentsPage }),
      },
      {
        path: 'data-connection/egress-policies',
        lazy: async () => ({ Component: (await import('./routes/data-connection/EgressPoliciesPage')).EgressPoliciesPage }),
      },
      {
        path: 'data-connection/new',
        lazy: async () => ({ Component: (await import('./routes/data-connection/NewSourcePage')).NewSourcePage }),
      },
      {
        path: 'data-connection/new/streaming',
        lazy: async () => ({ Component: (await import('./routes/data-connection/NewStreamingSourcePage')).NewStreamingSourcePage }),
      },
      {
        path: 'data-connection/sources/:id',
        lazy: async () => ({ Component: (await import('./routes/data-connection/SourceDetailPage')).SourceDetailPage }),
      },
      {
        path: 'projects',
        lazy: async () => ({ Component: (await import('./routes/projects/ProjectsListPage')).ProjectsListPage }),
      },
      {
        path: 'projects/:projectId',
        lazy: async () => ({ Component: (await import('./routes/projects/ProjectDetailPage')).ProjectDetailPage }),
      },
      {
        path: 'projects/:projectId/folders/:folderId',
        lazy: async () => ({ Component: (await import('./routes/projects/ProjectFolderPage')).ProjectFolderPage }),
      },
      {
        path: 'projects/:projectId/:folderId',
        lazy: async () => ({ Component: (await import('./routes/projects/ProjectFolderPage')).ProjectFolderPage }),
      },
      {
        path: 'ontology-manager',
        lazy: async () => ({ Component: (await import('./routes/ontology-manager/OntologyManagerPage')).OntologyManagerPage }),
      },
      {
        path: 'ontology-manager/bindings',
        lazy: async () => ({ Component: (await import('./routes/ontology-manager/BindingsWizardPage')).BindingsWizardPage }),
      },
      {
        path: 'ontology',
        lazy: async () => ({ Component: (await import('./routes/ontology/OntologyHomePage')).OntologyHomePage }),
      },
      {
        path: 'ontology/types',
        lazy: async () => ({ Component: (await import('./routes/ontology/CreateObjectTypePage')).CreateObjectTypePage }),
      },
      {
        path: 'ontology/graph',
        lazy: async () => ({ Component: (await import('./routes/ontology/OntologyGraphPage')).OntologyGraphPage }),
      },
      {
        path: 'ontology/object-sets',
        lazy: async () => ({ Component: (await import('./routes/ontology/ObjectSetsPage')).ObjectSetsPage }),
      },
      {
        path: 'ontology/:id',
        lazy: async () => ({ Component: (await import('./routes/ontology/ObjectTypeDetailPage')).ObjectTypeDetailPage }),
      },
      {
        path: 'charts-demo',
        lazy: async () => ({ Component: (await import('./routes/charts-demo/ChartsDemoPage')).ChartsDemoPage }),
      },
      {
        path: 'monaco-demo',
        lazy: async () => ({ Component: (await import('./routes/monaco-demo/MonacoDemoPage')).MonacoDemoPage }),
      },
      {
        path: 'maplibre-demo',
        lazy: async () => ({ Component: (await import('./routes/maplibre-demo/MapLibreDemoPage')).MapLibreDemoPage }),
      },
      {
        path: 'cytoscape-demo',
        lazy: async () => ({ Component: (await import('./routes/cytoscape-demo/CytoscapeDemoPage')).CytoscapeDemoPage }),
      },
      // Migration pattern: add a route here as you port each SvelteKit folder under apps/web/src/routes/.
      { path: '*', element: <NotFound /> },
    ],
  },
]);
