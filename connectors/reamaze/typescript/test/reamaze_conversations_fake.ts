// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject, encodeGoJSONValue } from '@superdurable/connectors-sdk/projectconfig';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';

/** The number of conversations Re:amaze lists per page. */
export const PageSize = 30;

/** One stored conversation. */
export interface StoredConversation { slug: string; subject: string; channel: string; status: number; createdAt: Date; body: string }
/** Optional fields of an inserted conversation. */
export interface InsertOptions { channel?: string; status?: number; createdAt?: Date; body?: string }

/** Go's "2006-01-02T15:04:05.000Z07:00" layout in UTC. */
const goMilliseconds = (moment: Date): string => moment.toISOString();
/** Go's time.RFC3339Nano in UTC, which trims trailing fractional zeros. */
const goRFC3339Nano = (moment: Date): string => moment.toISOString().replace(/\.?0+Z$/, 'Z');

function conversationWire(conversation: StoredConversation): Record<string, unknown> {
  const channel = conversation.channel || 'support';
  return {
    slug: conversation.slug, subject: conversation.subject, status: conversation.status, created_at: goMilliseconds(conversation.createdAt), tag_list: [],
    message: { body: conversation.body }, author: { name: 'Jane Smith', email: 'jane@acme.example.com' },
    category: { name: channel.slice(0, 1).toUpperCase() + channel.slice(1), slug: channel, channel: 1 },
  };
}

/**
 * A Re:amaze stand-in serving the conversation list, conversation read, and message list over HTTP Basic,
 * with response bytes identical to the Go connector's internal/reamazefake, so Go-recorded fetches replay
 * against it exactly. Pass send as ReamazeClientOptions.send.
 */
export class ReamazeConversationFake {
  /** The base URL to pass as ReamazeClientOptions.apiBaseURL. */ readonly baseURL = 'https://acme.reamaze.io/api/v1';
  private readonly authorization: string;
  private readonly stored: StoredConversation[] = [];
  private readonly hidden = new Set<string>();
  private isAscending = false;
  private readonly rateLimits: string[] = [];
  private listRequests = 0;
  /** The query of every answered list request, in order. */ readonly listedQueries: Array<Record<string, string>> = [];

  /** Accept email and token as HTTP Basic credentials. */
  constructor(email: string, token: string) { this.authorization = `Basic ${Buffer.from(`${email}:${token}`).toString('base64')}`; }

  /** Store a conversation; without createdAt it is now, truncated to milliseconds. */
  insert(slug: string, subject: string, options: InsertOptions = {}): void {
    this.stored.push({ slug, subject, channel: options.channel ?? '', status: options.status ?? 0, createdAt: options.createdAt ?? new Date(), body: options.body || `Opening message of ${subject}` });
  }
  /** Keep a stored conversation out of lists until reveal, as Re:amaze's delayed listing does. */ hide(slug: string): void { this.hidden.add(slug); }
  /** List a hidden conversation. */ reveal(slug: string): void { this.hidden.delete(slug); }
  /** Switch the list order to ascending creation time. */ listOldestFirst(): void { this.isAscending = true; }
  /** Answer the next list request with HTTP 429 and retryAfter, which may be empty. */ rateLimitNextList(retryAfter: string): void { this.rateLimits.push(retryAfter); }
  /** The number of list requests, including rate-limited ones. */ listRequestCount(): number { return this.listRequests; }

  /** Answer one provider request. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const url = new URL(request.url);
    const authorization = (request.headers as Record<string, string> | undefined)?.Authorization;
    if (authorization !== this.authorization) return errorResponse(401, '{"error":"unauthorized"}');
    if (request.method !== 'GET') return errorResponse(405, 'method not allowed');
    const path = url.pathname.replace(/^\/api\/v1/, '');
    if (path === '/conversations') return this.list(url.searchParams);
    if (path.startsWith('/conversations/') && path.endsWith('/messages')) return this.messages(path.slice('/conversations/'.length, -'/messages'.length));
    if (path.startsWith('/conversations/')) {
      const conversation = this.find(path.slice('/conversations/'.length));
      return conversation ? jsonResponse(conversationWire(conversation)) : errorResponse(404, '{"error":"not found"}');
    }
    return errorResponse(404, '404 page not found');
  };

  private list(query: URLSearchParams): ProviderResponse {
    this.listRequests++;
    const retryAfter = this.rateLimits.shift();
    if (retryAfter !== undefined) return errorResponse(429, '{"error":"rate limited"}', retryAfter ? { 'retry-after': retryAfter } : {});
    const parsed = Number.parseInt(query.get('page') ?? '', 10);
    const page = Number.isFinite(parsed) && parsed >= 1 ? parsed : 1;
    this.listedQueries.push(Object.fromEntries(query));
    const filter = query.get('filter') ?? '', category = query.get('category') ?? '';
    const matching = this.stored.filter(conversation => !this.hidden.has(conversation.slug) && !(conversation.status === 4 && filter !== 'all') && !(category && conversation.channel !== category));
    // Array.prototype.sort is stable, as Go's sort.SliceStable.
    matching.sort((left, right) => this.isAscending ? left.createdAt.getTime() - right.createdAt.getTime() : right.createdAt.getTime() - left.createdAt.getTime());
    const start = Math.min((page - 1) * PageSize, matching.length);
    return jsonResponse({ page_size: PageSize, page_count: Math.ceil(matching.length / PageSize), total_count: matching.length, conversations: matching.slice(start, start + PageSize).map(conversationWire) });
  }

  private messages(slug: string): ProviderResponse {
    const conversation = this.find(slug);
    if (!conversation) return errorResponse(404, '{"error":"not found"}');
    const message = { body: conversation.body, visibility: 0, origin: 1, created_at: goRFC3339Nano(conversation.createdAt), user: { name: 'Jane Smith', email: 'jane@acme.example.com' } };
    return jsonResponse({ page_size: PageSize, page_count: 1, total_count: 1, messages: [message] });
  }

  private find(slug: string): StoredConversation | undefined { return this.stored.find(conversation => conversation.slug === slug); }
}

// Go's json.Encoder sorts map keys, escapes HTML characters, and ends with a newline.
function jsonResponse(value: unknown): ProviderResponse {
  return { status: 200, headers: { 'content-type': 'application/json' }, body: Buffer.from(encodeGoJSONValue(value) + '\n') };
}
// As Go's http.Error writes it.
function errorResponse(status: number, body: string, headers: Record<string, string> = {}): ProviderResponse {
  return { status, headers: { 'content-type': 'text/plain; charset=utf-8', 'x-content-type-options': 'nosniff', ...headers }, body: Buffer.from(body + '\n') };
}

/** The email and API token the fake accepts in tests. */
export const FakeEmail = 'agent@acme.example.com';
// Split so push protection does not read a fixture as a token.
export const FakeToken = 'reamazeFake' + 'Token0123456789';

/**
 * A LoadedProject over one in-memory object store holding the connection's credential, its brand and Go
 * nanosecond pollInterval, and the given saved bindings; pass restartedObjects to model a restarted process.
 */
export async function newTestProject(connectionName: string, triggerBindings: Array<{ bindingName: string; configuration: Record<string, unknown> }>, restartedObjects?: InMemoryObjectStore): Promise<{ project: LoadedProject; objects: InMemoryObjectStore }> {
  const scope = { projectId: 'reamaze-trigger-tests', kind: 'live' } as const;
  const objects = restartedObjects ?? new InMemoryObjectStore();
  if (!restartedObjects) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'reamaze', connectionName }, 0, new CredentialMaterial({ email: FakeEmail, api_token: FakeToken }, undefined, 'apiKey'));
  const project = new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    // Dex Web stores a duration as Go nanoseconds.
    connections: [{ connectorId: 'reamaze', connectionName, modulePath: 'reamaze', provider: 'reamaze', configuration: { brand: 'acme', pollInterval: 10_000_000_000 } }],
    triggerBindings: triggerBindings.map(binding => ({ connectorId: 'reamaze', connectionName, triggerName: 'conversationCreated', ...binding })),
  }, undefined, objects, undefined, undefined);
  return { project, objects };
}
