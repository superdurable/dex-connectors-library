// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { GetCustomerDefinition } from './generated/definitions.js';
import type { GetCustomerInput, CustomerSummary } from './generated/models.js';
import type { OracleNetSuiteClient, OracleNetSuiteExchange } from './client.js';

const emptyCustomer = (): CustomerSummary => ({ id: '', entityId: '' });
const customerIDPattern = /^[1-9][0-9]*$/;

/** Read one customer by Oracle NetSuite internal ID. */
export class GetCustomerOperation implements Query<GetCustomerInput, CustomerSummary> {
  /** Bind the provider client. */ constructor(private readonly client: OracleNetSuiteClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetCustomerDefinition; }
  /** Project only the stable internal and entity IDs into the Flow. */
  async invoke(call: Call, input: GetCustomerInput): Promise<QueryAttempt<CustomerSummary>> {
    if (!input || typeof input.customerId !== 'string' || !customerIDPattern.test(input.customerId))
      return branch('defect', 'VALIDATION', 'customer internal ID is invalid');
    const exchange = await this.client.get(call, `/services/rest/record/v1/customer/${encodeURIComponent(input.customerId)}`);
    const failed = classifyRead(exchange);
    if (failed) return failed;
    const document = parseDocument(exchange.body);
    const result = document;
    if (!result || result.id !== input.customerId || typeof result.entityId !== 'string' || !result.entityId)
      return branch('invalidResponse', 'PROTOCOL', 'Oracle NetSuite returned invalid customer JSON');
    return newQueryBranch('found', { id: result.id, entityId: result.entityId });
  }
}

function classifyRead(exchange: OracleNetSuiteExchange): QueryAttempt<CustomerSummary> | undefined {
  if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'Oracle NetSuite connection needs authorization');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry('TRANSPORT', 'Oracle NetSuite customer request failed');
  if (exchange.status === 429) return retry('RATE_LIMIT', 'Oracle NetSuite rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Oracle NetSuite is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status === 404) return branch('notFound', 'NOT_FOUND', 'Oracle NetSuite customer was not found');
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Oracle NetSuite customer response exceeded the configured limit');
  if (exchange.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `Oracle NetSuite rejected HTTP ${exchange.status}`);
  return undefined;
}
function parseDocument(body: Uint8Array): Record<string, unknown> | undefined {
  try { const value: unknown = JSON.parse(Buffer.from(body).toString('utf8')); return isObject(value) ? value : undefined; }
  catch { return undefined; }
}
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function branch(branchId: string, kind: Failure['kind'], message: string): QueryAttempt<CustomerSummary> {
  return newQueryBranch(branchId, emptyCustomer(), {}, failure(kind, message));
}
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<CustomerSummary> {
  return newQueryRetry(failure(kind, message), delay);
}
function failure(kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'oracle-netsuite', operation: 'getCustomer', message };
}
