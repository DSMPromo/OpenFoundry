// Development Portal — Tier C from the AWS-integration planning
// discussion. Surfaces what's actually being built (issues, milestones,
// PRs) directly inside the OpenFoundry app so operators don't have to
// hop to github.com to track active work.
//
// Data comes from the public GitHub REST v3 API; lib/api/github.ts is
// the entire wire-shape declaration plus TanStack Query factories.
// Two-minute cache window keeps the request rate well under the
// 60 req/hr unauthenticated cap.

import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';

import {
  GITHUB_REPO,
  milestonesQuery,
  noMilestoneIssuesQuery,
  recentPullRequestsQuery,
  type GithubIssue,
  type GithubMilestone,
  type GithubPullRequest,
} from '@/lib/api/github';

export function DevelopmentPortalPage() {
  const milestones = useQuery(milestonesQuery);
  const looseIssues = useQuery(noMilestoneIssuesQuery);
  const pullRequests = useQuery(recentPullRequestsQuery);

  const loading = milestones.isLoading || looseIssues.isLoading || pullRequests.isLoading;
  const firstError = milestones.error ?? looseIssues.error ?? pullRequests.error;

  const sortedMilestones = sortMilestones(milestones.data ?? []);

  return (
    <section className="of-page" style={{ display: 'grid', gap: 20 }}>
      <header className="of-hero-strip">
        <div style={{ display: 'grid', gap: 8 }}>
          <p className="of-eyebrow">Control Panel · Development</p>
          <h1 className="of-heading-xl" style={{ margin: 0 }}>
            Development Portal
          </h1>
          <p className="of-text-muted" style={{ margin: 0, maxWidth: 720 }}>
            Live view of in-flight work on the OpenFoundry repo: milestones, open issues, and
            the most recent pull requests. Sourced from{' '}
            <a href={`https://github.com/${GITHUB_REPO}`} target="_blank" rel="noreferrer">
              {GITHUB_REPO}
            </a>{' '}
            via the public GitHub API.
          </p>
          <p style={{ fontSize: 12, color: 'var(--color-text-muted)', margin: 0 }}>
            Set <code>VITE_GITHUB_REPO</code> at build time to point the portal at a fork.
          </p>
        </div>
      </header>

      {firstError && (
        <div
          className="of-status-danger"
          style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}
        >
          GitHub fetch failed: {firstError.message}
        </div>
      )}

      {loading && !firstError && (
        <p className="of-text-muted" style={{ margin: 0 }}>
          Loading from GitHub…
        </p>
      )}

      {/* ---- Milestones ---- */}
      <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
        <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
          <h2 className="of-heading-md" style={{ margin: 0 }}>
            Milestones
          </h2>
          <span className="of-text-muted" style={{ fontSize: 12 }}>
            {sortedMilestones.length} total · {sortedMilestones.filter((m) => m.state === 'open').length} open
          </span>
        </header>
        {sortedMilestones.length === 0 && !loading && (
          <p className="of-text-muted" style={{ margin: 0 }}>
            No milestones yet. Create one with{' '}
            <code>gh api repos/{GITHUB_REPO}/milestones -X POST -f title=&quot;…&quot;</code>.
          </p>
        )}
        <div
          style={{
            display: 'grid',
            gap: 12,
            gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))',
          }}
        >
          {sortedMilestones.map((m) => (
            <MilestoneCard key={m.number} milestone={m} />
          ))}
        </div>
      </section>

      {/* ---- Issues without a milestone (orphans) ---- */}
      <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
        <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
          <h2 className="of-heading-md" style={{ margin: 0 }}>
            Unassigned open issues
          </h2>
          <span className="of-text-muted" style={{ fontSize: 12 }}>
            Not attached to any milestone
          </span>
        </header>
        <IssueList issues={filterOutPullRequests(looseIssues.data ?? [])} emptyHint="Everything open is tracked under a milestone — nice." />
      </section>

      {/* ---- Recent PRs ---- */}
      <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
        <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
          <h2 className="of-heading-md" style={{ margin: 0 }}>
            Recent pull requests
          </h2>
          <span className="of-text-muted" style={{ fontSize: 12 }}>
            Last 20 by update time
          </span>
        </header>
        <PullRequestList prs={pullRequests.data ?? []} />
      </section>

      <p style={{ fontSize: 12, color: 'var(--color-text-muted)', margin: 0 }}>
        <Link to="/control-panel">← Back to control panel</Link>
      </p>
    </section>
  );
}

// ---------------------------------------------------------------------
// Milestone card
// ---------------------------------------------------------------------

function MilestoneCard({ milestone }: { milestone: GithubMilestone }) {
  const total = milestone.open_issues + milestone.closed_issues;
  const pct = total === 0 ? 0 : Math.round((milestone.closed_issues / total) * 100);
  const stateLabel = milestone.state === 'open' ? 'OPEN' : 'CLOSED';
  return (
    <article
      style={{
        border: '1px solid var(--color-border)',
        borderRadius: 'var(--radius-md)',
        padding: 14,
        display: 'grid',
        gap: 8,
        background: 'var(--color-surface-elevated, transparent)',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 8 }}>
        <a href={milestone.html_url} target="_blank" rel="noreferrer" style={{ fontWeight: 600, textDecoration: 'none' }}>
          {milestone.title}
        </a>
        <span
          style={{
            fontSize: 10,
            padding: '2px 8px',
            borderRadius: 999,
            background: milestone.state === 'open' ? 'var(--color-status-info, #1f7ad7)20' : 'var(--color-status-success, #2f8f5b)20',
            color: milestone.state === 'open' ? 'var(--color-status-info, #1f7ad7)' : 'var(--color-status-success, #2f8f5b)',
            fontWeight: 600,
            letterSpacing: 0.4,
          }}
        >
          {stateLabel}
        </span>
      </div>
      {milestone.description && (
        <p className="of-text-muted" style={{ margin: 0, fontSize: 13, lineHeight: 1.4 }}>
          {milestone.description.length > 220 ? `${milestone.description.slice(0, 220)}…` : milestone.description}
        </p>
      )}
      <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
        <div
          aria-label={`${pct}% complete`}
          style={{
            flex: 1,
            height: 6,
            borderRadius: 999,
            background: 'var(--color-border)',
            overflow: 'hidden',
          }}
        >
          <div
            style={{
              width: `${pct}%`,
              height: '100%',
              background: 'var(--color-status-success, #2f8f5b)',
              transition: 'width 240ms ease',
            }}
          />
        </div>
        <span style={{ fontSize: 12, color: 'var(--color-text-muted)', minWidth: 96, textAlign: 'right' }}>
          {milestone.closed_issues}/{total} closed · {pct}%
        </span>
      </div>
      <MilestoneIssuesPreview milestoneNumber={milestone.number} />
    </article>
  );
}

function MilestoneIssuesPreview({ milestoneNumber }: { milestoneNumber: number }) {
  // Eagerly fetched when the card mounts so the user can scan the
  // current focus list without a click. Two-minute cache shares state
  // across cards if the same milestone is rendered twice.
  const { data, isLoading, error } = useQuery({
    queryKey: ['github', 'issues', GITHUB_REPO, 'milestone', milestoneNumber],
    queryFn: ({ signal }) =>
      import('@/lib/api/github').then((m) => m.listIssuesForMilestone(milestoneNumber, signal)),
    staleTime: 2 * 60 * 1000,
  });
  if (error) {
    return <p style={{ margin: 0, fontSize: 12, color: 'var(--color-status-danger, #c84a4a)' }}>Couldn’t load issues.</p>;
  }
  if (isLoading) {
    return <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>Loading issues…</p>;
  }
  const issues = filterOutPullRequests(data ?? []);
  const open = issues.filter((i) => i.state === 'open');
  if (open.length === 0) {
    return <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>All issues closed.</p>;
  }
  return (
    <ul style={{ margin: 0, padding: 0, listStyle: 'none', display: 'grid', gap: 4 }}>
      {open.slice(0, 5).map((issue) => (
        <li key={issue.number} style={{ fontSize: 12, display: 'flex', gap: 6, alignItems: 'baseline' }}>
          <span aria-hidden style={{ color: 'var(--color-status-info, #1f7ad7)' }}>○</span>
          <a href={issue.html_url} target="_blank" rel="noreferrer" style={{ flex: 1, textDecoration: 'none' }}>
            #{issue.number} {issue.title}
          </a>
        </li>
      ))}
      {open.length > 5 && (
        <li style={{ fontSize: 11, color: 'var(--color-text-muted)' }}>+{open.length - 5} more open…</li>
      )}
    </ul>
  );
}

// ---------------------------------------------------------------------
// Issue list (used by the "no milestone" pane)
// ---------------------------------------------------------------------

function IssueList({ issues, emptyHint }: { issues: GithubIssue[]; emptyHint: string }) {
  if (issues.length === 0) {
    return <p className="of-text-muted" style={{ margin: 0 }}>{emptyHint}</p>;
  }
  return (
    <ul style={{ margin: 0, padding: 0, listStyle: 'none', display: 'grid', gap: 6 }}>
      {issues.map((issue) => (
        <li key={issue.number} style={{ display: 'flex', alignItems: 'baseline', gap: 8 }}>
          <a href={issue.html_url} target="_blank" rel="noreferrer" style={{ flex: 1, textDecoration: 'none' }}>
            #{issue.number} {issue.title}
          </a>
          <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
            {issue.labels.slice(0, 3).map((label) => (
              <span
                key={label.name}
                style={{
                  fontSize: 10,
                  padding: '1px 6px',
                  borderRadius: 999,
                  background: `#${label.color || 'cccccc'}33`,
                  color: `#${label.color || '444444'}`,
                  border: `1px solid #${label.color || 'cccccc'}66`,
                }}
              >
                {label.name}
              </span>
            ))}
          </div>
        </li>
      ))}
    </ul>
  );
}

// ---------------------------------------------------------------------
// Pull request list
// ---------------------------------------------------------------------

function PullRequestList({ prs }: { prs: GithubPullRequest[] }) {
  if (prs.length === 0) {
    return <p className="of-text-muted" style={{ margin: 0 }}>No recent pull requests.</p>;
  }
  return (
    <ul style={{ margin: 0, padding: 0, listStyle: 'none', display: 'grid', gap: 6 }}>
      {prs.map((pr) => (
        <li key={pr.number} style={{ display: 'flex', alignItems: 'baseline', gap: 8 }}>
          <PullRequestStatusBadge pr={pr} />
          <a href={pr.html_url} target="_blank" rel="noreferrer" style={{ flex: 1, textDecoration: 'none' }}>
            #{pr.number} {pr.title}
          </a>
          {pr.user && (
            <span className="of-text-muted" style={{ fontSize: 11 }}>
              @{pr.user.login}
            </span>
          )}
        </li>
      ))}
    </ul>
  );
}

function PullRequestStatusBadge({ pr }: { pr: GithubPullRequest }) {
  let label = 'OPEN';
  let color = 'var(--color-status-info, #1f7ad7)';
  if (pr.merged_at) {
    label = 'MERGED';
    color = 'var(--color-status-success, #8a4ab0)';
  } else if (pr.state === 'closed') {
    label = 'CLOSED';
    color = 'var(--color-status-danger, #c84a4a)';
  } else if (pr.draft) {
    label = 'DRAFT';
    color = 'var(--color-text-muted, #888)';
  }
  return (
    <span
      style={{
        fontSize: 9,
        padding: '1px 6px',
        borderRadius: 999,
        background: `${color}20`,
        color,
        fontWeight: 700,
        letterSpacing: 0.4,
        minWidth: 54,
        textAlign: 'center',
      }}
    >
      {label}
    </span>
  );
}

// ---------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------

export function filterOutPullRequests(issues: GithubIssue[]): GithubIssue[] {
  return issues.filter((i) => !i.pull_request);
}

export function sortMilestones(milestones: GithubMilestone[]): GithubMilestone[] {
  // Open first, then closed; within each group most-recently-updated
  // wins so the active milestone leads the page.
  return [...milestones].sort((a, b) => {
    if (a.state !== b.state) return a.state === 'open' ? -1 : 1;
    return b.updated_at.localeCompare(a.updated_at);
  });
}
