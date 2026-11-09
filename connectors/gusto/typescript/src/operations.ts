// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { GetEmployeeDefinition } from './generated/definitions.js';
import type { Employee, GetEmployeeInput } from './generated/models.js';
import type { GustoClient, GustoExchange } from './client.js';

const emptyEmployee = (): Employee => ({ employeeId: '', firstName: '', lastName: '', email: '', companyId: '' });
const employeeUUIDPattern = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;

/** Read one employee from the authorized Gusto company. */
export class GetEmployeeOperation implements Query<GetEmployeeInput, Employee> {
  /** Bind the provider client. */ constructor(private readonly client: GustoClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetEmployeeDefinition; }
  /** Read one bounded response and project selected identity fields. */
  async invoke(call: Call, input: GetEmployeeInput): Promise<QueryAttempt<Employee>> {
    if (!input || !employeeUUIDPattern.test(input.employeeId))
      return branch('defect', 'VALIDATION', 'Gusto employee UUID is invalid');
    const exchange = await this.client.get(call, `/v1/employees/${input.employeeId}`);
    const failed = classifyRead(exchange);
    if (failed) return failed;
    const record = parseDocument(exchange.body);
    if (!record || typeof record.uuid !== 'string' || !employeeUUIDPattern.test(record.uuid) ||
      record.uuid.toLowerCase() !== input.employeeId.toLowerCase() ||
      typeof record.first_name !== 'string' || !record.first_name.trim() ||
      typeof record.last_name !== 'string' || !record.last_name.trim() ||
      record.email !== null && record.email !== undefined && typeof record.email !== 'string' ||
      typeof record.company_uuid !== 'string' || !employeeUUIDPattern.test(record.company_uuid))
      return branch('invalidResponse', 'PROTOCOL', 'Gusto returned invalid employee JSON');
    return newQueryBranch('found', { employeeId: record.uuid, firstName: record.first_name,
      lastName: record.last_name, email: typeof record.email === 'string' ? record.email : '', companyId: record.company_uuid });
  }
}

function classifyRead(exchange: GustoExchange): QueryAttempt<Employee> | undefined {
  if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'Gusto OAuth credentials are unavailable or invalid');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry('TRANSPORT', 'Gusto request failed');
  if (exchange.status === 429) return retry('RATE_LIMIT', 'Gusto rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Gusto is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Gusto response exceeded the configured limit');
  if (exchange.error) return retry('TRANSPORT', 'Gusto response was interrupted');
  if (exchange.status === 404) return newQueryBranch('missing', emptyEmployee());
  if (exchange.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `Gusto rejected HTTP ${exchange.status}`);
  return undefined;
}
function parseDocument(body: Uint8Array): Record<string, unknown> | undefined {
  try { const value: unknown = JSON.parse(Buffer.from(body).toString('utf8')); return isObject(value) ? value : undefined; }
  catch { return undefined; }
}
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function branch(branchId: string, kind: Failure['kind'], message: string): QueryAttempt<Employee> {
  return newQueryBranch(branchId, emptyEmployee(), {}, failure(kind, message));
}
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<Employee> {
  return newQueryRetry(failure(kind, message), delay);
}
function failure(kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'gusto', operation: 'getEmployee', message };
}
