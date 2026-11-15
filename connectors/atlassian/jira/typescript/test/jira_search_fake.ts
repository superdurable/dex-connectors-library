// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';

/**
 * A stateful stand-in for one Jira Cloud site's enhanced JQL search and issue reads, mirroring the Go
 * connector's internal/jirasearchfake: it understands exactly the JQL the issueChanged Trigger sends, encodes
 * page tokens as base64url "<offset>:<jql>", stamps responses with its own clock in a Date header, and formats
 * times in a non-UTC offset as Jira does. Pass send to JiraClient's send option.
 */

/** The fake site's cloudId. */
export const CloudID = '1324a887-45db-1bf4-1e99-ef0ff456d421';
/** One fake project. */
export interface Project { id: string; key: string; name: string }
/** The fake site's projects. */
export const Operations: Project = { id: '10000', key: 'OPS', name: 'Operations' };
export const HumanResources: Project = { id: '10001', key: 'HR', name: 'Human Resources' };
/** One recorded search request and the status the fake answered. */
export interface SearchRequest { jql: string; nextPageToken: string; maxResults: number; status: number }

const triggerJql = /^(?:project = ("?[A-Z0-9_]+"?) AND )?updated >= -([0-9]+)m ORDER BY updated (ASC|DESC)$/;
const jiraZoneOffsetMs = 3_600_000;

interface StoredIssue { id: string; key: string; summary: string; project: Project; created: number; updated: number; isUnindexed: boolean }

/** A fake site whose clock is settable. */
export class JiraSearchSite {
  private readonly issues = new Map<string, StoredIssue>();
  private nextIssueNumber = 1;
  private rateLimited = 0;
  private rateLimitStatus = 429;
  private retryAfterValue = '';
  private rejectsPageTokens = false;
  private readonly recorded: SearchRequest[] = [];
  /** Total requests of any kind, including issue reads. */ requestCount = 0;

  /** Create a fake whose clock returns Unix milliseconds. */
  constructor(private readonly clock: () => number = () => Date.now()) {}

  /** Add an issue to project and return its key, such as OPS-1. */
  create(project: Project, summary: string): string {
    const number = this.nextIssueNumber++;
    const now = this.clock();
    const issue = { id: String(20000 + number), key: `${project.key}-${number}`, summary, project, created: now, updated: now, isUnindexed: false };
    this.issues.set(issue.key, issue);
    return issue.key;
  }
  /** Change an issue's summary and move its updated time to the fake's clock. */
  update(key: string, summary: string): void { const issue = this.issues.get(key)!; issue.summary = summary; issue.updated = this.clock(); }
  /** Hide an issue from search, as Jira's index lag does, or show it again. */ setIndexed(key: string, isIndexed: boolean): void { this.issues.get(key)!.isUnindexed = !isIndexed; }
  /** Return the numeric ID of key. */ issueId(key: string): string { return this.issues.get(key)!.id; }
  /** Return the current version of key: its ID and its updated time as RFC 3339 UTC. */ issueVersion(key: string): { id: string; updatedAt: string } { const issue = this.issues.get(key)!; return { id: issue.id, updatedAt: new Date(issue.updated).toISOString() }; }
  /** Answer the next count searches with 429 and the Retry-After value. */ rateLimitNextRequests(count: number, retryAfter: string): void { this.rateLimited = count; this.rateLimitStatus = 429; this.retryAfterValue = retryAfter; }
  /** Answer the next count searches with 503 and the Retry-After value. */ overloadNextRequests(count: number, retryAfter: string): void { this.rateLimited = count; this.rateLimitStatus = 503; this.retryAfterValue = retryAfter; }
  /** Answer every search that carries a page token with 400. */ rejectPageTokens(isRejected: boolean): void { this.rejectsPageTokens = isRejected; }
  /** Return the recorded search requests in arrival order. */ requests(): SearchRequest[] { return [...this.recorded]; }

  /** Serve one provider request. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    this.requestCount++;
    const url = new URL(request.url);
    const prefix = `/ex/jira/${CloudID}/rest/api/3`;
    const authorization = Object.entries(request.headers ?? {}).find(([name]) => name.toLowerCase() === 'authorization')?.[1];
    if (!authorization || !url.pathname.startsWith(prefix)) return json(401, { errorMessages: ['unauthorized'] });
    const path = url.pathname.slice(prefix.length);
    if (request.method === 'POST' && path === '/search/jql') return this.serveSearch(request);
    if (request.method === 'GET' && path.startsWith('/issue/')) {
      const idOrKey = decodeURIComponent(path.slice('/issue/'.length));
      const issue = [...this.issues.values()].find(candidate => candidate.key === idOrKey || candidate.id === idOrKey);
      return issue ? json(200, issueResource(issue)) : json(404, { errorMessages: ['Issue does not exist'] });
    }
    return json(404, { errorMessages: ['not found'] });
  };

  private serveSearch(request: ProviderRequest): ProviderResponse {
    let body: { jql?: string; maxResults?: number; nextPageToken?: string };
    try { body = JSON.parse(Buffer.from(request.body ?? new Uint8Array()).toString('utf8')) as typeof body; }
    catch { return json(400, { errorMessages: ['invalid body'] }); }
    const jql = body.jql ?? '', maxResults = body.maxResults ?? 0, pageToken = body.nextPageToken ?? '';
    const headers: Record<string, string> = { date: new Date(this.clock()).toUTCString() };
    const [status, payload] = this.search(jql, maxResults, pageToken, headers);
    this.recorded.push({ jql, nextPageToken: pageToken, maxResults, status });
    return json(status, payload, headers);
  }

  private search(jql: string, maxResults: number, pageToken: string, headers: Record<string, string>): [number, unknown] {
    if (this.rateLimited > 0) {
      this.rateLimited--;
      headers['retry-after'] = this.retryAfterValue;
      return [this.rateLimitStatus, { errorMessages: ['rate limited'] }];
    }
    const match = triggerJql.exec(jql);
    let offset = 0;
    if (pageToken) {
      const decoded = Buffer.from(pageToken, 'base64url').toString('utf8');
      const separator = decoded.indexOf(':');
      const parsed = Number(decoded.slice(0, separator));
      if (this.rejectsPageTokens || separator < 0 || !Number.isInteger(parsed) || decoded.slice(separator + 1) !== jql) return [400, { errorMessages: ['invalid nextPageToken'] }];
      offset = parsed;
    }
    if (!match || maxResults < 1 || maxResults > 100) return [400, { errorMessages: ['unsupported JQL'] }];
    const since = Math.floor((this.clock() - Number(match[2]) * 60_000) / 60_000) * 60_000;
    const project = (match[1] ?? '').replace(/"/g, '');
    const isAscending = match[3] === 'ASC';
    const matched = [...this.issues.values()]
      .filter(issue => !issue.isUnindexed && issue.updated >= since && (!project || project === issue.project.id || project === issue.project.key))
      .sort((left, right) => left.updated !== right.updated ? (isAscending ? left.updated - right.updated : right.updated - left.updated) : (left.id < right.id ? -1 : left.id > right.id ? 1 : 0));
    const end = Math.min(offset + maxResults, matched.length);
    const page: Record<string, unknown> = { issues: matched.slice(Math.min(offset, end), end).map(issueResource) };
    if (end < matched.length) page.nextPageToken = Buffer.from(`${end}:${jql}`, 'utf8').toString('base64url');
    page.isLast = end >= matched.length;
    return [200, page];
  }
}

function jiraTime(millis: number): string {
  return new Date(millis + jiraZoneOffsetMs).toISOString().replace('Z', '+0100');
}

function issueResource(issue: StoredIssue): Record<string, unknown> {
  return { id: issue.id, key: issue.key, fields: {
    summary: issue.summary, status: { id: '1', name: 'To Do', statusCategory: { key: 'new' } }, issuetype: { id: '10001', name: 'Task' },
    project: { id: issue.project.id, key: issue.project.key, name: issue.project.name }, labels: [], created: jiraTime(issue.created), updated: jiraTime(issue.updated),
  } };
}

function json(status: number, payload: unknown, headers: Record<string, string> = {}): ProviderResponse {
  return { status, headers: { 'content-type': 'application/json', ...headers }, body: Buffer.from(JSON.stringify(payload)) };
}
