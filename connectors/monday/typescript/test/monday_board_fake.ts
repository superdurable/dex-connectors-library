// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// One monday.com board served over GraphQL for itemChanged tests, ported from Go's mondayboardfake. It answers
// items_page ordered by __last_updated__, next_items_page with single-use cursors, and items by ID. Every change
// advances the board clock by one second unless the test sets the clock itself.
import { SecretString } from '@superdurable/connectors-sdk';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { MondayClient } from '../src/client.js';
import type { CredentialSource, Credentials } from '../src/generated/models.js';

/** The personal API token the fake accepts. */
export const fakeToken = 'monday-fake-token';
const baseTime = Date.UTC(2026, 2, 2, 9, 0, 0);
const statusIndex: Record<string, number> = { 'Working on it': 0, Done: 1, Stuck: 2 };

interface FakeItem { id: string; name: string; status: string; createdAt: number; updatedAt: number }
/** One GraphQL request the fake answered. */
export interface RecordedRequest { operation: string; variables: Record<string, unknown>; status: number }

/** A stateful single-board fake. */
export class MondayBoardFake {
  /** Every answered request, in order. */ readonly requests: RecordedRequest[] = [];
  private clock = baseTime;
  private readonly items = new Map<string, FakeItem>();
  private readonly cursors = new Map<string, FakeItem[]>();
  private nextCursor = 0;
  private rateLimitedCalls = 0;
  private retryAfterSeconds = 0;
  private dailyLimitCalls = 0;

  /** Serve boardId. */
  constructor(readonly boardId: string) {}

  /** Add an active item with status Working on it. */
  create(itemId: string, name: string): void { const now = this.tick(); this.items.set(itemId, { id: itemId, name, status: 'Working on it', createdAt: now, updatedAt: now }); }
  /** Change an item's status label and move its last-updated time. */
  setStatus(itemId: string, status: string): void { const item = this.items.get(itemId)!; item.status = status; item.updatedAt = this.tick(); }
  /** Fix the time of the next change, such as a second change within the same second. */
  setClock(at: number): void { this.clock = at - 1000; }
  /** Answer the next count requests with HTTP 429, Retry-After, and RATE_LIMIT_EXCEEDED. */
  rateLimitNextRequests(count: number, retryAfterSeconds: number): void { this.rateLimitedCalls = count; this.retryAfterSeconds = retryAfterSeconds; }
  /** Answer the next count requests with DAILY_LIMIT_EXCEEDED. */
  exhaustDailyLimitForNextRequests(count: number): void { this.dailyLimitCalls = count; }
  /** An item's last-updated time in Unix milliseconds. */
  updatedAt(itemId: string): number { return this.items.get(itemId)!.updatedAt; }
  /** An item's last-updated time as Go's RFC 3339 seconds. */
  updatedAtText(itemId: string): string { return rfc3339(this.updatedAt(itemId)); }

  /** Answer one GraphQL request, as MondayClientOptions.send. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    let query = '', variables: Record<string, unknown> = {};
    try { const body = JSON.parse(Buffer.from(request.body ?? new Uint8Array()).toString('utf8')) as { query: string; variables?: Record<string, unknown> }; query = body.query; variables = body.variables ?? {}; }
    catch { /* An unparsable body is answered as GRAPHQL_PARSE_FAILED. */ }
    const fields = query.split(/\s+/);
    const operation = fields[0] === 'query' && fields[1] ? fields[1].split('(')[0]! : '';
    const headers: Record<string, string> = { 'content-type': 'application/json' };
    let status = 200, payload: unknown;
    if (request.method !== 'POST' || !query || !request.headers?.['API-Version']) { status = 400; payload = error('GRAPHQL_PARSE_FAILED'); }
    else if (request.headers.Authorization !== fakeToken) { status = 401; payload = error('UNAUTHORIZED'); }
    else if (this.rateLimitedCalls > 0) { this.rateLimitedCalls--; status = 429; headers['retry-after'] = String(this.retryAfterSeconds); payload = error('RATE_LIMIT_EXCEEDED', { retry_in_seconds: this.retryAfterSeconds }); }
    else if (this.dailyLimitCalls > 0) { this.dailyLimitCalls--; payload = error('DAILY_LIMIT_EXCEEDED'); }
    else if (operation === 'ListBoardItems') payload = this.listFirstPage(variables);
    else if (operation === 'ListNextBoardItems') payload = this.listNextPage(variables);
    else if (operation === 'GetItem') payload = { data: { items: ((variables.itemIds as unknown[]) ?? []).flatMap(id => this.items.has(String(id)) ? [this.resource(this.items.get(String(id))!)] : []) } };
    else payload = error('undefinedField');
    this.requests.push({ operation, variables, status });
    return { status, headers, body: Buffer.from(JSON.stringify(payload), 'utf8') };
  };

  private tick(): number { this.clock += 1000; return this.clock; }

  private listFirstPage(variables: Record<string, unknown>): unknown {
    const boardIds = variables.boardIds as unknown[] | undefined;
    if (boardIds?.length !== 1 || boardIds[0] !== this.boardId) return { data: { boards: [] } };
    if (JSON.stringify(variables.queryParams) !== '{"order_by":[{"column_id":"__last_updated__","direction":"desc"}]}') return error('InvalidArgumentException');
    const snapshot = [...this.items.values()].map(item => ({ ...item }))
      .sort((left, right) => left.updatedAt !== right.updatedAt ? right.updatedAt - left.updatedAt : (left.id < right.id ? 1 : left.id > right.id ? -1 : 0));
    return { data: { boards: [{ id: this.boardId, items_page: this.page(snapshot, limit(variables)) }] } };
  }

  private listNextPage(variables: Record<string, unknown>): unknown {
    const key = String(variables.cursor), remaining = this.cursors.get(key);
    if (!remaining) return error('InvalidArgumentException');
    this.cursors.delete(key);
    return { data: { next_items_page: this.page(remaining, limit(variables)) } };
  }

  private page(items: FakeItem[], size: number): unknown {
    let cursor: string | null = null;
    if (items.length > size) { cursor = `cursor-${++this.nextCursor}`; this.cursors.set(cursor, items.slice(size)); }
    return { cursor, items: items.slice(0, size).map(item => this.resource(item)) };
  }

  private resource(item: FakeItem): unknown {
    return {
      id: item.id, name: item.name, state: 'active', url: `https://contoso.monday.com/boards/${this.boardId}/pulses/${item.id}`,
      created_at: rfc3339(item.createdAt), updated_at: rfc3339(item.updatedAt), creator_id: '48202303',
      board: { id: this.boardId }, group: { id: 'topics', title: 'Topics' },
      column_values: [{ id: 'status', type: 'status', text: item.status, value: JSON.stringify({ index: statusIndex[item.status] ?? 0 }) }],
    };
  }
}

/** A personal-token credential source for the fake. */
export function fakeCredentials(): CredentialSource {
  const resolve = async (): Promise<Credentials> => ({ authMethodId: 'personal-api-token', apiToken: new SecretString(fakeToken) });
  return { resolve, resolveWithRefresh: resolve };
}

/** A client at the minimum poll interval whose requests send answers. */
export function newFakeClient(send: (request: ProviderRequest) => Promise<ProviderResponse>, now?: () => Date): MondayClient {
  return new MondayClient({ pollIntervalMs: 30_000 }, fakeCredentials(), { send, apiURL: 'http://127.0.0.1:9/v2', ...(now ? { now } : {}) });
}

function rfc3339(milliseconds: number): string { return new Date(milliseconds).toISOString().replace(/\.000Z$/, 'Z'); }
function limit(variables: Record<string, unknown>): number { return typeof variables.limit === 'number' && variables.limit >= 1 ? variables.limit : 25; }
function error(code: string, extensions: Record<string, unknown> = {}): unknown { return { errors: [{ message: code, extensions: { code, ...extensions } }] }; }

/** A loaded project with one personal-token monday.com connection, a 30s pollInterval unless connectionConfiguration replaces it, and itemChanged bindings; restartedObjects reopens earlier storage. */
export async function newTestProject(connectionName: string, triggerBindings: Array<{ bindingName: string; configuration: Record<string, unknown> }>, restartedObjects?: InMemoryObjectStore, connectionConfiguration: Record<string, unknown> = { pollInterval: 30_000_000_000 }): Promise<{ project: LoadedProject; objects: InMemoryObjectStore }> {
  const scope = { projectId: 'monday-trigger-tests', kind: 'live' } as const;
  const objects = restartedObjects ?? new InMemoryObjectStore();
  if (!restartedObjects) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'monday', connectionName }, 0,
    new CredentialMaterial({ auth_method: 'personal-api-token', api_token: fakeToken }, undefined, 'api_key'));
  const project = new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    // Dex Web stores a duration as Go nanoseconds.
    connections: [{ connectorId: 'monday', connectionName, modulePath: 'monday', provider: 'monday', configuration: connectionConfiguration }],
    triggerBindings: triggerBindings.map(binding => ({ connectorId: 'monday', connectionName, triggerName: 'itemChanged', ...binding })),
  }, undefined, objects, undefined, undefined);
  return { project, objects };
}
