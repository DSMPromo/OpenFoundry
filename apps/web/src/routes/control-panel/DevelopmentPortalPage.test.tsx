import { describe, expect, it } from 'vitest';

import {
  filterOutPullRequests,
  sortMilestones,
} from './DevelopmentPortalPage';
import type { GithubIssue, GithubMilestone } from '@/lib/api/github';

describe('filterOutPullRequests', () => {
  it('drops issues that GitHub has tagged as pull requests', () => {
    const issues: GithubIssue[] = [
      makeIssue(1, 'real issue'),
      { ...makeIssue(2, 'PR pretending to be an issue'), pull_request: { html_url: 'x' } },
      makeIssue(3, 'another real issue'),
    ];
    const out = filterOutPullRequests(issues);
    expect(out.map((i) => i.number)).toEqual([1, 3]);
  });
});

describe('sortMilestones', () => {
  it('floats open milestones to the top, sorts each group by updated_at desc', () => {
    const milestones: GithubMilestone[] = [
      makeMilestone(1, 'old closed', 'closed', '2025-01-01T00:00:00Z'),
      makeMilestone(2, 'new closed', 'closed', '2026-03-01T00:00:00Z'),
      makeMilestone(3, 'old open', 'open', '2025-06-01T00:00:00Z'),
      makeMilestone(4, 'new open', 'open', '2026-05-01T00:00:00Z'),
    ];
    const out = sortMilestones(milestones);
    expect(out.map((m) => m.number)).toEqual([4, 3, 2, 1]);
  });

  it('does not mutate the input array', () => {
    const milestones: GithubMilestone[] = [
      makeMilestone(1, 'a', 'open', '2026-01-01T00:00:00Z'),
      makeMilestone(2, 'b', 'open', '2026-02-01T00:00:00Z'),
    ];
    const before = milestones.map((m) => m.number).join(',');
    sortMilestones(milestones);
    expect(milestones.map((m) => m.number).join(',')).toBe(before);
  });
});

// --- helpers -----------------------------------------------------------

function makeIssue(number: number, title: string): GithubIssue {
  return {
    number,
    title,
    state: 'open',
    html_url: `https://github.com/x/y/issues/${number}`,
    labels: [],
    pull_request: null,
    user: null,
    updated_at: '2026-05-01T00:00:00Z',
    milestone: null,
  };
}

function makeMilestone(
  number: number,
  title: string,
  state: 'open' | 'closed',
  updatedAt: string,
): GithubMilestone {
  return {
    number,
    title,
    description: null,
    state,
    open_issues: 0,
    closed_issues: 0,
    html_url: `https://github.com/x/y/milestone/${number}`,
    due_on: null,
    updated_at: updatedAt,
  };
}
