// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ListAccessibleCustomersDefinition } from './generated/definitions.js';
import type { AccessibleCustomers, ListAccessibleCustomersInput } from './generated/models.js';
import type { AdsClient, AdsExchange } from './client.js';

const emptyCustomers = (): AccessibleCustomers => ({ resourceNames: [] });

/** Read directly accessible customer names with a local result bound. */
export class ListAccessibleCustomersOperation implements Query<ListAccessibleCustomersInput, AccessibleCustomers> {
  /** Bind the provider client. */ constructor(private readonly client: AdsClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListAccessibleCustomersDefinition; }
  /** Fetch and validate bounded customer resource names. */
  async invoke(call: Call, input: ListAccessibleCustomersInput): Promise<QueryAttempt<AccessibleCustomers>> {
    if (!input || (input.maxCustomers !== undefined && (!Number.isSafeInteger(input.maxCustomers) || input.maxCustomers < 0 || input.maxCustomers > 10000)))
      return branch('defect', 'VALIDATION', 'maximum customer count is invalid');
    const maxCustomers = input.maxCustomers || 1000;
    const exchange = await this.client.get(call, '/v26/customers:listAccessibleCustomers');
    const failed = classifyRead(exchange);
    if (failed) return failed;
    const document = parseDocument(exchange.body);
    const names = document?.resourceNames ?? [];
    if (!document || !Array.isArray(names) || names.length > maxCustomers)
      return branch('invalidResponse', 'PROTOCOL', 'Google Ads returned invalid customer JSON');
    const seen = new Set<string>();
    for (const name of names) {
      if (typeof name !== 'string' || !/^customers\/[0-9]{1,20}$/.test(name) || seen.has(name))
        return branch('invalidResponse', 'PROTOCOL', 'Google Ads returned invalid customer JSON');
      seen.add(name);
    }
    return newQueryBranch('listed', { resourceNames: names as string[] });
  }
}

function classifyRead(exchange: AdsExchange): QueryAttempt<AccessibleCustomers> | undefined {
  if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry('TRANSPORT', 'Google Ads request failed');
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Google Ads response exceeded the configured limit');
  if (exchange.status === 429) return retry('RATE_LIMIT', 'Google Ads rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Google Ads is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `Google Ads rejected HTTP ${exchange.status}`);
  return undefined;
}
function parseDocument(body: Uint8Array): Record<string, unknown> | undefined {
  try { const value: unknown = JSON.parse(Buffer.from(body).toString('utf8')); return isObject(value) ? value : undefined; }
  catch { return undefined; }
}
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function branch(branchId: string, kind: Failure['kind'], message: string): QueryAttempt<AccessibleCustomers> {
  return newQueryBranch(branchId, emptyCustomers(), {}, failure(kind, message));
}
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<AccessibleCustomers> {
  return newQueryRetry(failure(kind, message), delay);
}
function failure(kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'google-ads', operation: 'listAccessibleCustomers', message };
}
