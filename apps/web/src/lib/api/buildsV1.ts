// D1.1.5 Builds — typed client for the Foundry-aligned `/v1/builds`
// surface implemented by `pipeline-build-service` (P1 → P4).
//
// The legacy `/api/v1/data-integration/builds` endpoints live in
// `pipelines.ts` and continue to back the per-pipeline runs page.

export type BuildState =
	| 'BUILD_RESOLUTION'
	| 'BUILD_QUEUED'
	| 'BUILD_RUNNING'
	| 'BUILD_ABORTING'
	| 'BUILD_FAILED'
	| 'BUILD_ABORTED'
	| 'BUILD_COMPLETED';

export type AbortPolicy = 'DEPENDENT_ONLY' | 'ALL_NON_DEPENDENT';

export type TriggerKind = 'MANUAL' | 'SCHEDULED' | 'FORCE';

export type JobState =
	| 'WAITING'
	| 'RUN_PENDING'
	| 'RUNNING'
	| 'ABORT_PENDING'
	| 'ABORTED'
	| 'FAILED'
	| 'COMPLETED';

export interface Build {
	id: string;
	rid: string;
	pipeline_rid: string;
	build_branch: string;
	job_spec_fallback: string[];
	state: BuildState;
	trigger_kind: TriggerKind;
	force_build: boolean;
	abort_policy: AbortPolicy;
	queued_at?: string | null;
	started_at?: string | null;
	finished_at?: string | null;
	error_message?: string | null;
	log_uri?: string | null;
	requested_by: string;
	created_at: string;
}

export interface Job {
	id: string;
	rid: string;
	build_id: string;
	job_spec_rid: string;
	state: JobState;
	output_transaction_rids: string[];
	state_changed_at: string;
	attempt: number;
	stale_skipped: boolean;
	failure_reason?: string | null;
	output_content_hash?: string | null;
	created_at: string;
}

export interface BuildEnvelope extends Build {
	jobs: Job[];
}

export interface ListBuildsParams {
	branch?: string;
	status?: BuildState | string;
	pipeline_rid?: string;
	since?: string;
	until?: string;
	cursor?: string;
	limit?: number;
}

export interface ListBuildsResponse {
	data: Build[];
	next_cursor: string | null;
	limit: number;
}

export interface JobOutputRow {
	output_dataset_rid: string;
	transaction_rid: string;
	committed: boolean;
	aborted: boolean;
}

export interface JobOutputsResponse {
	rid: string;
	state: JobState;
	stale_skipped: boolean;
	outputs: JobOutputRow[];
}

export interface InputResolutionRow {
	dataset_rid: string;
	branch: string;
	filter:
		| { kind: 'AT_TIMESTAMP'; value: string }
		| { kind: 'AT_TRANSACTION'; transaction_rid: string }
		| { kind: 'RANGE'; from_transaction: string; to_transaction: string }
		| { kind: 'INCREMENTAL_SINCE_LAST_BUILD' };
	resolved_view_id?: string;
	resolved_transaction_rid?: string;
	range_from_transaction_rid?: string;
	range_to_transaction_rid?: string;
	note?: string;
}

export interface JobInputResolutionsResponse {
	rid: string;
	input_view_resolutions: InputResolutionRow[];
}

export type JobLogLevel = 'TRACE' | 'DEBUG' | 'INFO' | 'WARN' | 'ERROR' | 'FATAL' | string;

export interface JobLogEntry {
	sequence: number;
	ts: string;
	level: JobLogLevel;
	message: string;
	params?: unknown;
}

export interface JobLogsResponse {
	data: JobLogEntry[];
	total: number;
}

export interface ListJobLogsParams {
	from_sequence?: number;
	since?: string;
	until?: string;
	levels?: JobLogLevel[];
	limit?: number;
}

export interface CreateBuildRequest {
	pipeline_rid: string;
	build_branch: string;
	job_spec_fallback?: string[];
	force_build?: boolean;
	output_dataset_rids: string[];
	trigger_kind?: TriggerKind;
	abort_policy?: AbortPolicy;
}

export interface CreateBuildResponse {
	build_id: string;
	state: BuildState;
	queued_reason: string | null;
	job_count: number;
	output_transactions: Array<{ dataset_rid: string; transaction_rid: string }>;
}

const BASE = '/v1';

async function jsonFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
	const res = await fetch(`${BASE}${path}`, {
		credentials: 'include',
		headers: {
			'Content-Type': 'application/json',
			...(init.headers ?? {})
		},
		...init
	});
	if (!res.ok) {
		const detail = await res.text().catch(() => '');
		throw new Error(`${res.status} ${res.statusText}: ${detail}`);
	}
	return (await res.json()) as T;
}

export function listBuildsV1(params: ListBuildsParams = {}): Promise<ListBuildsResponse> {
	const qs = new URLSearchParams();
	for (const [k, v] of Object.entries(params)) {
		if (v !== undefined && v !== null && String(v).length > 0) qs.set(k, String(v));
	}
	const query = qs.toString();
	return jsonFetch<ListBuildsResponse>(`/builds${query ? `?${query}` : ''}`);
}

export function getBuildV1(rid: string): Promise<BuildEnvelope> {
	return jsonFetch<BuildEnvelope>(`/builds/${encodeURIComponent(rid)}`);
}

export function abortBuildV1(rid: string): Promise<{ rid: string; state: BuildState }> {
	return jsonFetch(`/builds/${encodeURIComponent(rid)}:abort`, { method: 'POST' });
}

export function listDatasetBuildsV1(
	datasetRid: string
): Promise<{ data: Build[]; dataset_rid: string }> {
	return jsonFetch(`/datasets/${encodeURIComponent(datasetRid)}/builds`);
}

export function getJobOutputsV1(jobRid: string): Promise<JobOutputsResponse> {
	return jsonFetch(`/jobs/${encodeURIComponent(jobRid)}/outputs`);
}

export function getJobInputResolutionsV1(jobRid: string): Promise<JobInputResolutionsResponse> {
	return jsonFetch(`/jobs/${encodeURIComponent(jobRid)}/input-resolutions`);
}

export function listJobLogsV1(jobRid: string, params: ListJobLogsParams = {}): Promise<JobLogsResponse> {
	const qs = new URLSearchParams();
	for (const [k, v] of Object.entries(params)) {
		if (v === undefined || v === null) continue;
		if (Array.isArray(v)) {
			if (v.length > 0) qs.set(k, v.join(','));
		} else if (String(v).length > 0) {
			qs.set(k, String(v));
		}
	}
	const query = qs.toString();
	return jsonFetch<JobLogsResponse>(`/jobs/${encodeURIComponent(jobRid)}/logs${query ? `?${query}` : ''}`);
}

// Build-keyed merged log surface (A4.2 / A4.3). The build SSE channel
// multiplexes every job's stream, so a single subscription tails the
// whole build instead of pinning to a job that may not exist yet.

export interface BuildLogArchive {
	build_id: string;
	log_uri: string;
	state: BuildState;
}

export function listBuildLogsV1(buildRid: string, params: ListJobLogsParams = {}): Promise<JobLogsResponse> {
	const qs = new URLSearchParams();
	for (const [k, v] of Object.entries(params)) {
		if (v === undefined || v === null) continue;
		if (Array.isArray(v)) {
			if (v.length > 0) qs.set(k, v.join(','));
		} else if (String(v).length > 0) {
			qs.set(k, String(v));
		}
	}
	const query = qs.toString();
	return jsonFetch<JobLogsResponse>(`/builds/${encodeURIComponent(buildRid)}/logs${query ? `?${query}` : ''}`);
}

export function buildLogsStreamUrl(buildRid: string, params: ListJobLogsParams = {}): string {
	const qs = new URLSearchParams();
	for (const [k, v] of Object.entries(params)) {
		if (v === undefined || v === null) continue;
		if (Array.isArray(v)) {
			if (v.length > 0) qs.set(k, v.join(','));
		} else if (String(v).length > 0) {
			qs.set(k, String(v));
		}
	}
	const query = qs.toString();
	return `${BASE}/builds/${encodeURIComponent(buildRid)}/logs/stream${query ? `?${query}` : ''}`;
}

export async function getBuildLogArchiveV1(buildRid: string): Promise<BuildLogArchive | null> {
	const res = await fetch(`${BASE}/builds/${encodeURIComponent(buildRid)}/logs/archive`, { credentials: 'include' });
	if (res.status === 404) return null;
	if (!res.ok) {
		const detail = await res.text().catch(() => '');
		throw new Error(`${res.status} ${res.statusText}: ${detail}`);
	}
	return (await res.json()) as BuildLogArchive;
}

export function runBuildV1(body: CreateBuildRequest): Promise<CreateBuildResponse> {
	return jsonFetch(`/builds`, { method: 'POST', body: JSON.stringify(body) });
}

// Color tokens for badge rendering. Keyed by canonical BuildState.
export const BUILD_STATE_COLORS: Record<BuildState, { bg: string; text: string; pulse?: boolean }> = {
	BUILD_RESOLUTION: { bg: '#1d4ed8', text: '#dbeafe' },
	BUILD_QUEUED: { bg: '#374151', text: '#e5e7eb' },
	BUILD_RUNNING: { bg: '#0891b2', text: '#cffafe', pulse: true },
	BUILD_ABORTING: { bg: '#b45309', text: '#fef3c7', pulse: true },
	BUILD_FAILED: { bg: '#b91c1c', text: '#fecaca' },
	BUILD_ABORTED: { bg: '#92400e', text: '#fde68a' },
	BUILD_COMPLETED: { bg: '#166534', text: '#bbf7d0' }
};

export const JOB_STATE_COLORS: Record<JobState, { bg: string; text: string }> = {
	WAITING: { bg: '#374151', text: '#e5e7eb' },
	RUN_PENDING: { bg: '#1d4ed8', text: '#dbeafe' },
	RUNNING: { bg: '#0891b2', text: '#cffafe' },
	ABORT_PENDING: { bg: '#b45309', text: '#fef3c7' },
	ABORTED: { bg: '#92400e', text: '#fde68a' },
	FAILED: { bg: '#b91c1c', text: '#fecaca' },
	COMPLETED: { bg: '#166534', text: '#bbf7d0' }
};
