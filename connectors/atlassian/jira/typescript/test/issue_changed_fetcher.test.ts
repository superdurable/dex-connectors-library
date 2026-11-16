// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { newTriggerContext, SecretString, type TriggerEvent } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { describe, expect, it } from 'vitest';
import { JiraClient } from '../src/client.js';
import { decodeIssueChangeCursor, encodeIssueChangeCursor } from '../src/issue_change_cursor.js';
import { IssueChangeFetcher } from '../src/issue_change_fetcher.js';
import type { IssueChangedEvent, IssueChangedTriggerConfiguration } from '../src/issue_changed.js';
import { CloudID, HumanResources, JiraSearchSite, Operations } from './jira_search_fake.js';

interface GoScenario { tokens: string[]; eventIds: string[] | null; kinds: string[] | null; keys: string[] | null; hasMore: boolean[] | null; requests: { jql: string; nextPageToken: string; status: number }[] }
const goCases = JSON.parse(readFileSync(new URL('./fixtures/go-issue-change-cases.json', import.meta.url), 'utf8')) as { scenarios: Record<string, GoScenario> };
const connection = { provider: 'atlassian', name: 'jira-trigger-test' };
const credentials = { oauthClientId: 'client-id', oauthClientSecret: new SecretString('client-secret'), accessToken: new SecretString('jira-trigger-token'), refreshToken: new SecretString('refresh-token') };
const source = { resolve: async () => credentials, resolveWithRefresh: async () => credentials };
type Page = ChangePage<TriggerEvent<IssueChangedEvent>>;

/** One fake site, a client on a shared settable clock, and a recorder of what the Go fixtures hold. */
class Environment {
  now = Date.UTC(2026, 10, 2, 9, 0, 0);
  readonly site = new JiraSearchSite(() => this.now);
  readonly client: JiraClient;
  readonly context = newTriggerContext();
  readonly recorded = { tokens: [] as string[], eventIds: [] as string[], kinds: [] as string[], keys: [] as string[], hasMore: [] as boolean[] };

  constructor(localSkewMs = 0, send = this.site.send) {
    this.client = new JiraClient({ cloudId: CloudID, endpoint: 'http://127.0.0.1:12345', pollIntervalMs: 5_000 }, source, { send, now: () => new Date(this.now + localSkewMs) });
  }
  advance(milliseconds: number): void { this.now += milliseconds; }
  fetcher(configuration: IssueChangedTriggerConfiguration = { projectId: Operations.id }): IssueChangeFetcher { return new IssueChangeFetcher(this.client, connection, configuration); }
  async seed(fetcher: IssueChangeFetcher): Promise<Cursor> { const cursor = await fetcher.seedCursor(this.context); this.recorded.tokens.push(cursor.token); return cursor; }
  async fetch(fetcher: IssueChangeFetcher, cursor: Cursor): Promise<Page> {
    const page = await fetcher.fetchChanges(this.context, cursor);
    this.recorded.tokens.push(page.next.token);
    this.recorded.hasMore.push(page.hasMorePages ?? false);
    for (const change of page.changes) { this.recorded.eventIds.push(change.id); this.recorded.kinds.push(change.payload.changeKind); this.recorded.keys.push(change.payload.issue.key); }
    return page;
  }
  expectGoParity(scenario: string): void {
    const expected = goCases.scenarios[scenario]!;
    expect(this.recorded.tokens).toEqual(expected.tokens);
    expect(this.recorded.eventIds).toEqual(expected.eventIds ?? []);
    expect(this.recorded.kinds).toEqual(expected.kinds ?? []);
    expect(this.recorded.keys).toEqual(expected.keys ?? []);
    expect(this.recorded.hasMore).toEqual(expected.hasMore ?? []);
    expect(this.site.requests().map(request => ({ jql: request.jql, nextPageToken: request.nextPageToken, status: request.status }))).toEqual(expected.requests);
  }
}

const keys = (page: Page): string[] => page.changes.map(change => change.payload.issue.key);

describe('issueChanged fetcher at parity with Go', () => {
  it('seeds a baseline that marks recent issues seen and delivers none', async () => {
    const environment = new Environment();
    environment.site.create(Operations, 'Old issue');
    environment.advance(3_600_000);
    const recent = environment.site.create(Operations, 'Recent issue');
    const fetcher = environment.fetcher();
    const cursor = await fetcher.seedCursor(environment.context);
    const position = decodeIssueChangeCursor(cursor);
    expect([position.cloudId, position.project, position.updatedThrough]).toEqual([CloudID, 'id:10000', environment.now]);
    expect(position.seenVersions).toEqual([`${environment.site.issueId(recent)}@${environment.now}`]);
    expect(environment.site.requests()[0]!.jql).toBe('project = 10000 AND updated >= -4m ORDER BY updated DESC');
    environment.advance(60_000);
    const page = await fetcher.fetchChanges(environment.context, cursor);
    expect(page.changes).toEqual([]);
    expect(page.next.token).toBe(cursor.token);
  });

  it('delivers created and updated issues once with Go-identical cursors and event IDs', async () => {
    const environment = new Environment();
    const existing = environment.site.create(Operations, 'Existing');
    environment.advance(600_000);
    const fetcher = environment.fetcher();
    const cursor = await environment.seed(fetcher);
    environment.advance(1_000);
    const created = environment.site.create(Operations, 'Loading dock light out');
    environment.site.create(HumanResources, 'Other project');
    environment.advance(1_000);
    environment.site.update(existing, 'Existing, edited');
    environment.advance(1_000);
    const page = await environment.fetch(fetcher, cursor);
    expect(keys(page)).toEqual([created, existing]);
    expect(page.changes.map(change => change.payload.changeKind)).toEqual(['created', 'updated']);
    expect(page.changes[1]!.payload.issue.summary).toBe('Existing, edited');
    const refetched = await fetcher.fetchChanges(environment.context, cursor);
    expect(refetched.changes.map(change => change.id)).toEqual(page.changes.map(change => change.id));
    environment.advance(30_000);
    const after = await environment.fetch(fetcher, page.next);
    expect(after.changes).toEqual([]);
    environment.site.update(created, 'Loading dock light replaced');
    const again = await environment.fetch(fetcher, after.next);
    expect(keys(again)).toEqual([created]);
    expect(again.changes[0]!.id).not.toBe(page.changes[0]!.id);
    const expected = goCases.scenarios.seedAndFetch!;
    expect(environment.recorded.tokens).toEqual(expected.tokens);
    expect(environment.recorded.eventIds).toEqual(expected.eventIds);
  });

  it('delivers an update indexed late within the lookback', async () => {
    const environment = new Environment();
    const fetcher = environment.fetcher();
    const cursor = await fetcher.seedCursor(environment.context);
    environment.advance(1_000);
    const lagging = environment.site.create(Operations, 'Indexed late');
    environment.site.setIndexed(lagging, false);
    environment.advance(1_000);
    const prompt = environment.site.create(Operations, 'Indexed at once');
    const first = await fetcher.fetchChanges(environment.context, cursor);
    expect(keys(first)).toEqual([prompt]);
    environment.site.setIndexed(lagging, true);
    environment.advance(30_000);
    expect(keys(await fetcher.fetchChanges(environment.context, first.next))).toEqual([lagging]);
  });

  it('pages and restarts after a rejected page token without loss', async () => {
    const environment = new Environment();
    const fetcher = environment.fetcher();
    const cursor = await environment.seed(fetcher);
    for (let index = 0; index < 150; index++) { environment.advance(1); environment.site.create(Operations, `Bulk ${index}`); }
    const first = await environment.fetch(fetcher, cursor);
    expect([first.changes.length, first.hasMorePages]).toEqual([100, true]);
    environment.site.rejectPageTokens(true);
    const second = await environment.fetch(fetcher, first.next);
    expect([second.changes.length, second.hasMorePages]).toEqual([0, true]);
    environment.site.rejectPageTokens(false);
    const third = await environment.fetch(fetcher, second.next);
    expect([third.changes.length, third.hasMorePages]).toEqual([50, false]);
    environment.expectGoParity('rejectedPageToken');
  });

  it('restarts a page sequence older than the lookback', async () => {
    const environment = new Environment();
    const fetcher = environment.fetcher();
    const cursor = await environment.seed(fetcher);
    for (let index = 0; index < 120; index++) { environment.advance(1); environment.site.create(Operations, `Bulk ${index}`); }
    const first = await environment.fetch(fetcher, cursor);
    environment.advance(121_000);
    const second = await environment.fetch(fetcher, first.next);
    expect(environment.site.requests().at(-1)!.jql).toBe('project = 10000 AND updated >= -6m ORDER BY updated ASC');
    expect(second.changes).toEqual([]);
    expect((await environment.fetch(fetcher, second.next)).changes).toHaveLength(20);
    environment.expectGoParity('staleSequence');
  });

  it('loses no issue when an update shifts a paged backlog', async () => {
    const environment = new Environment();
    const fetcher = environment.fetcher();
    const cursor = await environment.seed(fetcher);
    const created: string[] = [];
    for (let index = 0; index < 150; index++) { environment.advance(12_000); created.push(environment.site.create(Operations, `Backlog ${index}`)); }
    const first = await environment.fetch(fetcher, cursor);
    environment.site.update(created[10]!, 'Backlog 10, edited');
    const delivered = new Set(keys(first));
    let page = first, isEditDelivered = false;
    while (page.hasMorePages) {
      page = await environment.fetch(fetcher, page.next);
      keys(page).forEach(key => delivered.add(key));
      isEditDelivered ||= page.changes.some(change => change.payload.issue.summary === 'Backlog 10, edited');
    }
    expect(created.every(key => delivered.has(key)) && isEditDelivered).toBe(true);
    expect(decodeIssueChangeCursor(first.next).pageToken).toBe('');
    environment.expectGoParity('backlogShift');
  });

  it("uses Jira's clock when the local clock runs ahead", async () => {
    const environment = new Environment(600_250);
    const fetcher = environment.fetcher({});
    const cursor = await environment.seed(fetcher);
    environment.advance(1_000);
    environment.site.create(Operations, 'Created right after the baseline');
    environment.site.create(HumanResources, 'Other project');
    await environment.fetch(fetcher, cursor);
    await environment.seed(environment.fetcher({ projectKey: 'HR' }));
    environment.expectGoParity('skewedClock');
  });

  it.each([['rateLimit', 7], ['overload', 11]] as const)('pauses the connection on %s with Retry-After', async (kind, seconds) => {
    const environment = new Environment();
    const fetcher = environment.fetcher();
    const cursor = await fetcher.seedCursor(environment.context);
    if (kind === 'rateLimit') environment.site.rateLimitNextRequests(1, String(seconds)); else environment.site.overloadNextRequests(1, String(seconds));
    const error = await fetcher.fetchChanges(environment.context, cursor).catch((caught: unknown) => caught);
    expect(error).toBeInstanceOf(RateLimitedError);
    expect((error as RateLimitedError).retryAfterMs).toBe(seconds * 1000);
    expect(isCursorInvalid(error)).toBe(false);
  });

  it('marks foreign or undecodable cursors invalid', async () => {
    const environment = new Environment();
    const cursor = await environment.fetcher().seedCursor(environment.context);
    expect(isCursorInvalid(await environment.fetcher({ projectKey: 'HR' }).fetchChanges(environment.context, cursor).catch((error: unknown) => error))).toBe(true);
    const foreign = encodeIssueChangeCursor({ ...decodeIssueChangeCursor(cursor), cloudId: '99999999-0000-4000-8000-000000000000' });
    expect(isCursorInvalid(await environment.fetcher().fetchChanges(environment.context, foreign).catch((error: unknown) => error))).toBe(true);
    expect(isCursorInvalid(await environment.fetcher().fetchChanges(environment.context, { token: 'not json' }).catch((error: unknown) => error))).toBe(true);
  });

  it('reports rejections and unavailable credentials', async () => {
    const environment = new Environment();
    const cursor = await environment.fetcher({}).seedCursor(environment.context);
    const rejecting = new Environment(0, async () => ({ status: 403, headers: {}, body: Buffer.from('{}') }));
    await expect(rejecting.fetcher({}).fetchChanges(rejecting.context, cursor)).rejects.toThrow('Jira rejected the issue search with HTTP 403');
    const missing = new JiraClient({ cloudId: CloudID, endpoint: 'http://127.0.0.1:12345' }, { resolve: async () => { throw new Error('no credential'); }, resolveWithRefresh: async () => { throw new Error('no credential'); } }, { send: environment.site.send });
    await expect(new IssueChangeFetcher(missing, connection, {}).seedCursor(environment.context)).rejects.toThrow('credentials are unavailable');
    expect(() => new JiraClient({ cloudId: CloudID, pollIntervalMs: 4_999 }, source)).toThrow('pollInterval must be at least 5s');
    expect(new JiraClient({ cloudId: CloudID, pollIntervalMs: 0 }, source).pollIntervalMs).toBe(30_000);
  });
});
