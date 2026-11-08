// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ListAdAccountsDefinition } from './generated/definitions.js';
import type { AdAccountsPage, ListAdAccountsInput } from './generated/models.js';
import type { AdsClient, AdsExchange } from './client.js';

const emptyPage = (): AdAccountsPage => ({ accounts: [], nextPageToken: '' });
const safePageToken = (token: string): boolean => token.length <= 1024 && [...token].every(character => {
  const code = character.codePointAt(0) ?? 0;
  return code >= 33 && code <= 126;
});

/** Read one cursor-based page of LinkedIn ad account identities. */
export class ListAdAccountsOperation implements Query<ListAdAccountsInput, AdAccountsPage> {
  /** Bind the provider client. */ constructor(private readonly client: AdsClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListAdAccountsDefinition; }
  /** Fetch and validate one bounded ad account page. */
  async invoke(call: Call, input: ListAdAccountsInput): Promise<QueryAttempt<AdAccountsPage>> {
    if (!input || (input.pageSize !== undefined && (!Number.isSafeInteger(input.pageSize) || input.pageSize < 0 || input.pageSize > 1000)) ||
      (input.pageToken !== undefined && (typeof input.pageToken !== 'string' || !safePageToken(input.pageToken))))
      return branch('defect', 'VALIDATION', 'account page size or cursor is invalid');
    const pageSize = input.pageSize || 100;
    const query = new URLSearchParams({ q: 'search', pageSize: String(pageSize) });
    if (input.pageToken) query.set('pageToken', input.pageToken);
    const exchange = await this.client.get(call, `/rest/adAccounts?${query.toString()}`);
    const failed = classifyRead(exchange);
    if (failed) return failed;
    const document = parseDocument(exchange.body);
    const elements = document?.elements;
    const metadata = document?.metadata ?? {};
    const nextPageToken = isObject(metadata) ? metadata.nextPageToken ?? '' : undefined;
    if (!Array.isArray(elements) || elements.length > pageSize || typeof nextPageToken !== 'string' || !safePageToken(nextPageToken))
      return branch('invalidResponse', 'PROTOCOL', 'LinkedIn Ads returned invalid account JSON');
    const accounts: AdAccountsPage['accounts'] = [];
    const seen = new Set<number>();
    for (const item of elements) {
      if (!isObject(item) || typeof item.id !== 'number' || !Number.isSafeInteger(item.id) || item.id <= 0 ||
        typeof item.name !== 'string' || !item.name.trim() || seen.has(item.id))
        return branch('invalidResponse', 'PROTOCOL', 'LinkedIn Ads returned invalid account JSON');
      seen.add(item.id);
      accounts.push({ id: String(item.id), name: item.name });
    }
    return newQueryBranch('listed', { accounts, nextPageToken });
  }
}

function classifyRead(exchange: AdsExchange): QueryAttempt<AdAccountsPage> | undefined {
  if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry('TRANSPORT', 'LinkedIn Ads request failed');
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'LinkedIn Ads response exceeded the configured limit');
  if (exchange.status === 429) return retry('RATE_LIMIT', 'LinkedIn Ads rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'LinkedIn Ads is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `LinkedIn Ads rejected HTTP ${exchange.status}`);
  return undefined;
}
function parseDocument(body: Uint8Array): Record<string, unknown> | undefined {
  try { const value: unknown = JSON.parse(Buffer.from(body).toString('utf8')); return isObject(value) ? value : undefined; }
  catch { return undefined; }
}
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function branch(branchId: string, kind: Failure['kind'], message: string): QueryAttempt<AdAccountsPage> {
  return newQueryBranch(branchId, emptyPage(), {}, failure(kind, message));
}
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<AdAccountsPage> {
  return newQueryRetry(failure(kind, message), delay);
}
function failure(kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'linkedin-ads', operation: 'listAdAccounts', message };
}
