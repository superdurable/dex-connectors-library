// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ListAccountsDefinition } from './generated/definitions.js';
import type { AccountsPage, ListAccountsInput } from './generated/models.js';
import type { BasecampClient, BasecampExchange } from './client.js';

const emptyPage = (): AccountsPage => ({ accounts: [], offset: 0, limit: 0, total: 0 });

/** Read one bounded local page of OAuth-authorized Basecamp accounts. */
export class ListAccountsOperation implements Query<ListAccountsInput, AccountsPage> {
  /** Bind the provider client. */ constructor(private readonly client: BasecampClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListAccountsDefinition; }
  /** Project account IDs and company names from a bounded JSON response. */
  async invoke(call: Call, input: ListAccountsInput): Promise<QueryAttempt<AccountsPage>> {
    if (!input || !Number.isSafeInteger(input.offset) || input.offset < 0 ||
      (input.limit !== undefined && (!Number.isSafeInteger(input.limit) || input.limit < 0 || input.limit > 100)))
      return branch('defect', 'VALIDATION', 'offset or limit is invalid');
    const limit = input.limit || 100;
    const exchange = await this.client.get(call, '/authorization.json');
    const failed = classifyRead(exchange);
    if (failed) return failed;
    const document = parseDocument(exchange.body);
    if (!document || !Array.isArray(document.accounts))
      return branch('invalidResponse', 'PROTOCOL', 'Basecamp returned invalid account JSON');
    const accounts: AccountsPage['accounts'] = [];
    const seen = new Set<number>();
    for (const item of document.accounts) {
      if (!isObject(item)) return branch('invalidResponse', 'PROTOCOL', 'Basecamp returned invalid account JSON');
      if (item.product !== 'bc3') continue;
      if (typeof item.id !== 'number' || !Number.isSafeInteger(item.id) || item.id <= 0 || typeof item.name !== 'string' || !item.name || seen.has(item.id))
        return branch('invalidResponse', 'PROTOCOL', 'Basecamp returned invalid account JSON');
      seen.add(item.id);
      accounts.push({ id: String(item.id), company: item.name });
    }
    return newQueryBranch('listed', { accounts: accounts.slice(input.offset, input.offset + limit), offset: input.offset, limit, total: accounts.length });
  }
}

function classifyRead(exchange: BasecampExchange): QueryAttempt<AccountsPage> | undefined {
  if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry('TRANSPORT', 'Basecamp request failed');
  // Retryable statuses win over their body, so an oversized 429 or 5xx page still retries.
  if (exchange.status === 429) return retry('RATE_LIMIT', 'Basecamp rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Basecamp is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Basecamp response exceeded the configured limit');
  if (exchange.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `Basecamp rejected HTTP ${exchange.status}`);
  return undefined;
}
function parseDocument(body: Uint8Array): Record<string, unknown> | undefined {
  try { const value: unknown = JSON.parse(Buffer.from(body).toString('utf8')); return isObject(value) ? value : undefined; }
  catch { return undefined; }
}
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function branch(branchId: string, kind: Failure['kind'], message: string): QueryAttempt<AccountsPage> {
  return newQueryBranch(branchId, emptyPage(), {}, failure(kind, message));
}
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<AccountsPage> {
  return newQueryRetry(failure(kind, message), delay);
}
function failure(kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'basecamp', operation: 'listAccounts', message };
}
