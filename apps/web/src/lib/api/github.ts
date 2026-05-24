// Public GitHub REST client used by the in-app Development Portal.
// Reads-only — no auth required for public repos. The portal speaks
// directly to api.github.com from the browser; if you fork OpenFoundry
// into a private repo, set VITE_GITHUB_TOKEN at build time so the
// client adds the Authorization header.
//
// All shapes are intentionally a narrow subset of the GitHub REST v3
// payload — only the fields the portal actually renders are declared.

const DEFAULT_REPO = 'DSMPromo/OpenFoundry';

const repoFromEnv = (import.meta.env.VITE_GITHUB_REPO as string | undefined)?.trim();
const tokenFromEnv = (import.meta.env.VITE_GITHUB_TOKEN as string | undefined)?.trim();

export const GITHUB_REPO = repoFromEnv && repoFromEnv.length > 0 ? repoFromEnv : DEFAULT_REPO;

const GITHUB_API = 'https://api.github.com';

export interface GithubMilestone {
  number: number;
  title: string;
  description: string | null;
  state: 'open' | 'closed';
  open_issues: number;
  closed_issues: number;
  html_url: string;
  due_on: string | null;
  updated_at: string;
}

export interface GithubLabel {
  name: string;
  color: string;
}

export interface GithubIssue {
  number: number;
  title: string;
  state: 'open' | 'closed';
  html_url: string;
  labels: GithubLabel[];
  // GitHub returns a `pull_request` field on issues that ARE PRs.
  // The portal hides PRs from the issue list (they're shown in their
  // own pane) — so this discriminator is part of the wire shape.
  pull_request?: { html_url: string } | null;
  user: { login: string; avatar_url: string } | null;
  updated_at: string;
  milestone: { number: number } | null;
}

export interface GithubPullRequest {
  number: number;
  title: string;
  state: 'open' | 'closed';
  merged_at: string | null;
  html_url: string;
  user: { login: string; avatar_url: string } | null;
  draft: boolean;
  created_at: string;
}

/**
 * Performs a GET against api.github.com with optional bearer auth from
 * VITE_GITHUB_TOKEN. The browser-side fetch is rate-limited to 60
 * requests/hour without a token (5,000 with one) — we cache responses
 * via TanStack Query so the portal stays well within both limits.
 */
async function ghFetch<T>(path: string, signal?: AbortSignal): Promise<T> {
  const headers: Record<string, string> = {
    Accept: 'application/vnd.github+json',
    'X-GitHub-Api-Version': '2022-11-28',
  };
  if (tokenFromEnv) headers.Authorization = `Bearer ${tokenFromEnv}`;
  const res = await fetch(`${GITHUB_API}${path}`, { headers, signal });
  if (!res.ok) {
    const detail = await safeReadText(res);
    throw new Error(`GitHub ${res.status} ${res.statusText}${detail ? `: ${detail}` : ''}`);
  }
  return (await res.json()) as T;
}

async function safeReadText(res: Response): Promise<string> {
  try {
    const t = await res.text();
    return t.length > 200 ? `${t.slice(0, 200)}…` : t;
  } catch {
    return '';
  }
}

export function listMilestones(signal?: AbortSignal): Promise<GithubMilestone[]> {
  // state=all so closed milestones still surface in the portal — the
  // user wants to see history, not just what's open.
  return ghFetch<GithubMilestone[]>(`/repos/${GITHUB_REPO}/milestones?state=all&per_page=50`, signal);
}

export function listIssuesForMilestone(milestoneNumber: number, signal?: AbortSignal): Promise<GithubIssue[]> {
  return ghFetch<GithubIssue[]>(
    `/repos/${GITHUB_REPO}/issues?milestone=${milestoneNumber}&state=all&per_page=100`,
    signal,
  );
}

export function listIssuesNoMilestone(signal?: AbortSignal): Promise<GithubIssue[]> {
  return ghFetch<GithubIssue[]>(`/repos/${GITHUB_REPO}/issues?milestone=none&state=open&per_page=50`, signal);
}

export function listRecentPullRequests(signal?: AbortSignal): Promise<GithubPullRequest[]> {
  return ghFetch<GithubPullRequest[]>(
    `/repos/${GITHUB_REPO}/pulls?state=all&sort=updated&direction=desc&per_page=20`,
    signal,
  );
}

// ---------------------------------------------------------------------
// TanStack Query factories. The Portal page uses these directly so
// keys + fetchers stay co-located with the wire shapes.
// ---------------------------------------------------------------------

export const githubQueryKeys = {
  all: ['github'] as const,
  milestones: () => [...githubQueryKeys.all, 'milestones', GITHUB_REPO] as const,
  issuesByMilestone: (number: number) =>
    [...githubQueryKeys.all, 'issues', GITHUB_REPO, 'milestone', number] as const,
  issuesNoMilestone: () => [...githubQueryKeys.all, 'issues', GITHUB_REPO, 'no-milestone'] as const,
  pullRequests: () => [...githubQueryKeys.all, 'pulls', GITHUB_REPO] as const,
};

// Two minutes of staleness on the GitHub views is fine — the portal is
// for human watching, not for tight CI feedback.
const TWO_MINUTES = 2 * 60 * 1000;

export const milestonesQuery = {
  queryKey: githubQueryKeys.milestones(),
  queryFn: ({ signal }: { signal?: AbortSignal }) => listMilestones(signal),
  staleTime: TWO_MINUTES,
};

export function milestoneIssuesQuery(milestoneNumber: number) {
  return {
    queryKey: githubQueryKeys.issuesByMilestone(milestoneNumber),
    queryFn: ({ signal }: { signal?: AbortSignal }) => listIssuesForMilestone(milestoneNumber, signal),
    staleTime: TWO_MINUTES,
  };
}

export const noMilestoneIssuesQuery = {
  queryKey: githubQueryKeys.issuesNoMilestone(),
  queryFn: ({ signal }: { signal?: AbortSignal }) => listIssuesNoMilestone(signal),
  staleTime: TWO_MINUTES,
};

export const recentPullRequestsQuery = {
  queryKey: githubQueryKeys.pullRequests(),
  queryFn: ({ signal }: { signal?: AbortSignal }) => listRecentPullRequests(signal),
  staleTime: TWO_MINUTES,
};
