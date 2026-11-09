// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ListAccountsDefinition } from './generated/definitions.js';
import type { AccountsPage, ListAccountsInput } from './generated/models.js';
import type { AWeberClient, AWeberExchange } from './client.js';

const emptyPage = (): AccountsPage => ({ accounts: [], offset: 0, limit: 0, total: 0 });

/** Read one page of the OAuth-authorized customer's accounts. */
export class ListAccountsOperation implements Query<ListAccountsInput, AccountsPage> {
  /** Bind the provider client. */ constructor(private readonly client: AWeberClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListAccountsDefinition; }
  /** Project account IDs and company names from a bounded JSON response. */
  async invoke(call: Call, input: ListAccountsInput): Promise<QueryAttempt<AccountsPage>> {
    if (!input || !Number.isSafeInteger(input.offset) || input.offset < 0 ||
      (input.limit !== undefined && (!Number.isSafeInteger(input.limit) || input.limit < 0 || input.limit > 100)))
      return branch('defect', 'VALIDATION', 'offset or limit is invalid');
    const limit = input.limit || 100;
    const query = new URLSearchParams({ 'ws.size': String(limit), 'ws.start': String(input.offset) });
    const exchange = await this.client.get(call, `/1.0/accounts?${query.toString()}`);
    const failed = classifyRead(exchange);
    if (failed) return failed;
    const document = parseDocument(exchange.body);
    if (!document || !Array.isArray(document.entries) || typeof document.start !== 'number' || !Number.isSafeInteger(document.start) ||
      document.start !== input.offset || typeof document.total_size !== 'number' || !Number.isSafeInteger(document.total_size) || document.total_size < document.entries.length)
      return branch('invalidResponse', 'PROTOCOL', 'AWeber returned invalid account JSON');
    const accounts: AccountsPage['accounts'] = [];
    for (const item of document.entries) {
      if (!isObject(item) || typeof item.id !== 'number' || !Number.isSafeInteger(item.id) || item.id <= 0 || item.id > 2_147_483_647 || typeof item.company !== 'string')
        return branch('invalidResponse', 'PROTOCOL', 'AWeber returned invalid account JSON');
      accounts.push({ id: String(item.id), company: item.company });
    }
    return newQueryBranch('listed', { accounts, offset: input.offset, limit, total: document.total_size });
  }
}

function classifyRead(exchange: AWeberExchange): QueryAttempt<AccountsPage> | undefined {
  if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry('TRANSPORT', 'AWeber request failed');
  // Retryable statuses win over their body, so an oversized 429 or 5xx page still retries.
  if (exchange.status === 429) return retry('RATE_LIMIT', 'AWeber rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'AWeber is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'AWeber response exceeded the configured limit');
  if (exchange.status === 403 && isRateLimit(exchange.body)) return retry('RATE_LIMIT', 'AWeber rate limited the request', exchange.retryAfterMs);
  if (exchange.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `AWeber rejected HTTP ${exchange.status}`);
  return undefined;
}
function isRateLimit(body: Uint8Array): boolean {
  const document = parseDocument(body);
  return isObject(document?.error) && document.error.type === 'ForbiddenError' &&
    typeof document.error.message === 'string' && document.error.message.toLowerCase() === 'rate limit error';
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
  return { kind, provider: 'aweber', operation: 'listAccounts', message };
}
