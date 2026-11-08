// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ListPinsDefinition } from './generated/definitions.js';
import type { PinsPage, ListPinsInput } from './generated/models.js';
import type { PinterestClient, PinterestExchange } from './client.js';

const emptyPage = (): PinsPage => ({ pins: [], nextBookmark: '' });

/** Read one cursor-based page of the authorized user's Pins. */
export class ListPinsOperation implements Query<ListPinsInput, PinsPage> {
  /** Bind the provider client. */ constructor(private readonly client: PinterestClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListPinsDefinition; }
  /** Project Pin IDs, board IDs, titles, and the next bookmark. */
  async invoke(call: Call, input: ListPinsInput): Promise<QueryAttempt<PinsPage>> {
    if (!input || (input.pageSize !== undefined && (!Number.isSafeInteger(input.pageSize) || input.pageSize < 0 || input.pageSize > 250)) ||
      (input.bookmark !== undefined && !validBookmark(input.bookmark)))
      return branch('defect', 'VALIDATION', 'page size or bookmark is invalid');
    const pageSize = input.pageSize || 25;
    const query = new URLSearchParams();
    if (input.bookmark) query.set('bookmark', input.bookmark);
    query.set('page_size', String(pageSize));
    const exchange = await this.client.get(call, `/v5/pins?${query.toString()}`);
    const failed = classifyRead(exchange);
    if (failed) return failed;
    const document = parseDocument(exchange.body);
    if (!document || !Array.isArray(document.items) || document.items.length > pageSize ||
      (document.bookmark !== undefined && document.bookmark !== null && !validBookmark(document.bookmark)))
      return branch('invalidResponse', 'PROTOCOL', 'Pinterest returned invalid Pin JSON');
    const pins: PinsPage['pins'] = [];
    for (const item of document.items) {
      if (!isObject(item) || !numericID(item.id) || !numericID(item.board_id) ||
        (item.title !== undefined && item.title !== null && typeof item.title !== 'string'))
        return branch('invalidResponse', 'PROTOCOL', 'Pinterest returned invalid Pin JSON');
      pins.push({ id: item.id, boardId: item.board_id, title: item.title ?? '' });
    }
    return newQueryBranch('listed', { pins, nextBookmark: typeof document.bookmark === 'string' ? document.bookmark : '' });
  }
}

function classifyRead(exchange: PinterestExchange): QueryAttempt<PinsPage> | undefined {
  if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry('TRANSPORT', 'Pinterest request failed');
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Pinterest response exceeded the configured limit');
  if (exchange.status === 429) return retry('RATE_LIMIT', 'Pinterest rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Pinterest is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `Pinterest rejected HTTP ${exchange.status}`);
  return undefined;
}
function numericID(value: unknown): value is string { return typeof value === 'string' && /^[0-9]{1,32}$/.test(value); }
function validBookmark(value: unknown): value is string { return typeof value === 'string' && Buffer.byteLength(value) <= 4096 && !/[\r\n]/.test(value); }
function parseDocument(body: Uint8Array): Record<string, unknown> | undefined {
  try { const value: unknown = JSON.parse(Buffer.from(body).toString('utf8')); return isObject(value) ? value : undefined; }
  catch { return undefined; }
}
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function branch(branchId: string, kind: Failure['kind'], message: string): QueryAttempt<PinsPage> {
  return newQueryBranch(branchId, emptyPage(), {}, failure(kind, message));
}
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<PinsPage> {
  return newQueryRetry(failure(kind, message), delay);
}
function failure(kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'pinterest', operation: 'listPins', message };
}
