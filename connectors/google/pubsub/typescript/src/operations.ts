// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ListTopicsDefinition } from './generated/definitions.js';
import type { TopicsPage, ListTopicsInput } from './generated/models.js';
import type { PubSubClient, PubSubExchange } from './client.js';

const emptyPage = (): TopicsPage => ({ topics: [], nextPageToken: '' });

/** Read one bounded topic page in a Google Cloud project. */
export class ListTopicsOperation implements Query<ListTopicsInput, TopicsPage> {
  /** Bind the provider client. */ constructor(private readonly client: PubSubClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListTopicsDefinition; }
  /** Project topic resource names and the next page token. */
  async invoke(call: Call, input: ListTopicsInput): Promise<QueryAttempt<TopicsPage>> {
    if (!input || !validProjectID(input.projectId) ||
      (input.pageSize !== undefined && (!Number.isSafeInteger(input.pageSize) || input.pageSize < 0 || input.pageSize > 100)) ||
      (input.pageToken !== undefined && !validPageToken(input.pageToken)))
      return branch('defect', 'VALIDATION', 'project ID, page size, or page token is invalid');
    const pageSize = input.pageSize || 100;
    const query = new URLSearchParams({ pageSize: String(pageSize) });
    if (input.pageToken) query.set('pageToken', input.pageToken);
    const path = `/v1/projects/${encodeURIComponent(input.projectId)}/topics?${query.toString()}`;
    const exchange = await this.client.get(call, path);
    const failed = classifyRead(exchange);
    if (failed) return failed;
    const document = parseDocument(exchange.body);
    const entries = document?.topics ?? [];
    if (!document || !Array.isArray(entries) || entries.length > pageSize ||
      (document.nextPageToken !== undefined && document.nextPageToken !== null && !validPageToken(document.nextPageToken)))
      return branch('invalidResponse', 'PROTOCOL', 'PubSub returned invalid topic JSON');
    const topics: TopicsPage['topics'] = [];
    const prefix = `projects/${input.projectId}/topics/`;
    for (const item of entries) {
      if (!isObject(item) || typeof item.name !== 'string' || !item.name.startsWith(prefix) ||
        !validTopicID(item.name.slice(prefix.length)))
        return branch('invalidResponse', 'PROTOCOL', 'PubSub returned invalid topic JSON');
      topics.push({ name: item.name });
    }
    return newQueryBranch('listed', { topics, nextPageToken: typeof document.nextPageToken === 'string' ? document.nextPageToken : '' });
  }
}

function classifyRead(exchange: PubSubExchange): QueryAttempt<TopicsPage> | undefined {
  if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry('TRANSPORT', 'PubSub request failed');
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'PubSub response exceeded the configured limit');
  if (exchange.status === 429) return retry('RATE_LIMIT', 'PubSub rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'PubSub is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `PubSub rejected HTTP ${exchange.status}`);
  return undefined;
}
function validProjectID(value: unknown): value is string { return typeof value === 'string' && /^[A-Za-z0-9][A-Za-z0-9:._~-]{0,255}$/.test(value); }
function validTopicID(value: unknown): value is string { return typeof value === 'string' && value.length > 0 && value.length <= 255 && !/[/\r\n]/.test(value); }
function validPageToken(value: unknown): value is string { return typeof value === 'string' && Buffer.byteLength(value) <= 4096 && !/[\r\n]/.test(value); }
function parseDocument(body: Uint8Array): Record<string, unknown> | undefined {
  try { const value: unknown = JSON.parse(Buffer.from(body).toString('utf8')); return isObject(value) ? value : undefined; }
  catch { return undefined; }
}
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function branch(branchId: string, kind: Failure['kind'], message: string): QueryAttempt<TopicsPage> {
  return newQueryBranch(branchId, emptyPage(), {}, failure(kind, message));
}
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<TopicsPage> {
  return newQueryRetry(failure(kind, message), delay);
}
function failure(kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'google-pubsub', operation: 'listTopics', message };
}
