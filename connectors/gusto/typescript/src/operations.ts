// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { GetEmployeeDefinition, ListEmployeesDefinition } from './generated/definitions.js';
import type { Employee, EmployeesPage, GetEmployeeInput, ListEmployeesInput } from './generated/models.js';
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
      return branch('getEmployee', emptyEmployee(), 'defect', 'VALIDATION', 'Gusto employee UUID is invalid');
    const exchange = await this.client.get(call, `/v1/employees/${input.employeeId}`);
    const failed = classifyRead(exchange, 'getEmployee', emptyEmployee());
    if (failed) return failed;
    const employee = decodeEmployee(parseDocument(exchange.body));
    if (!employee || employee.employeeId.toLowerCase() !== input.employeeId.toLowerCase())
      return branch('getEmployee', emptyEmployee(), 'invalidResponse', 'PROTOCOL', 'Gusto returned invalid employee JSON');
    return newQueryBranch('found', employee);
  }
}

/** Read a one-based page of employee identities from the authorized Gusto company. */
export class ListEmployeesOperation implements Query<ListEmployeesInput, EmployeesPage> {
  /** Bind the authenticated provider client. */
  constructor(private readonly client: GustoClient) {}

  /** Return the manifest-generated pagination contract. */
  definition(): QueryDefinition { return ListEmployeesDefinition; }

  /** Return employee identity fields for a company and one positive page of up to 100 employees. */
  async invoke(call: Call, input: ListEmployeesInput): Promise<QueryAttempt<EmployeesPage>> {
    const empty: EmployeesPage = { employees: [] };
    if (!input || typeof input.companyId !== 'string' || !employeeUUIDPattern.test(input.companyId) ||
        !Number.isSafeInteger(input.page) || input.page < 1 ||
        !Number.isSafeInteger(input.perPage) || input.perPage < 1 || input.perPage > 100)
      return branch('listEmployees', empty, 'defect', 'VALIDATION', 'Gusto company UUID or page settings are invalid');
    const path = `/v1/companies/${input.companyId}/employees?page=${input.page}&per=${input.perPage}`;
    const exchange = await this.client.get(call, path);
    const failed = classifyRead(exchange, 'listEmployees', empty);
    if (failed) return failed;
    const page = decodeEmployeesPage(parseDocument(exchange.body), input.companyId);
    if (!page) return branch('listEmployees', empty, 'invalidResponse', 'PROTOCOL', 'Gusto returned invalid employee page JSON');
    return newQueryBranch('listed', page);
  }
}

function decodeEmployeesPage(document: unknown, companyId: string): EmployeesPage | undefined {
  if (!Array.isArray(document) || document.length > 100) return undefined;
  const employees: Employee[] = [];
  for (const record of document) {
    const employee = decodeEmployee(record);
    if (!employee || employee.companyId.toLowerCase() !== companyId.toLowerCase()) return undefined;
    employees.push(employee);
  }
  return { employees };
}

function decodeEmployee(record: unknown): Employee | undefined {
  if (!isObject(record) || typeof record.uuid !== 'string' || !employeeUUIDPattern.test(record.uuid) ||
      typeof record.first_name !== 'string' || !record.first_name.trim() ||
      typeof record.last_name !== 'string' || !record.last_name.trim() ||
      record.email != null && typeof record.email !== 'string' ||
      typeof record.company_uuid !== 'string' || !employeeUUIDPattern.test(record.company_uuid)) return undefined;
  return { employeeId: record.uuid, firstName: record.first_name,
    lastName: record.last_name, email: record.email ?? '', companyId: record.company_uuid };
}

function classifyRead<T>(exchange: GustoExchange, operation: string, empty: T): QueryAttempt<T> | undefined {
  if (exchange.error === 'credential') return branch(operation, empty, 'defect', 'AUTHENTICATION', 'Gusto OAuth credentials are unavailable or invalid');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry(operation, 'TRANSPORT', 'Gusto request failed');
  if (exchange.status === 429) return retry(operation, 'RATE_LIMIT', 'Gusto rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry(operation, 'AVAILABILITY', 'Gusto is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.error === 'tooLarge') return branch(operation, empty, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'Gusto response exceeded the configured limit');
  if (exchange.error) return retry(operation, 'TRANSPORT', 'Gusto response was interrupted');
  if (exchange.status === 404) return newQueryBranch('missing', empty);
  if (exchange.status !== 200) return branch(operation, empty, 'providerRejected', 'PROVIDER_REJECTION', `Gusto rejected HTTP ${exchange.status}`);
  return undefined;
}
function parseDocument(body: Uint8Array): unknown {
  try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; }
  catch { return undefined; }
}
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function branch<T>(operation: string, empty: T, branchId: string, kind: Failure['kind'], message: string): QueryAttempt<T> {
  return newQueryBranch(branchId, empty, {}, failure(operation, kind, message));
}
function retry<T>(operation: string, kind: Failure['kind'], message: string, delay = 0): QueryAttempt<T> {
  return newQueryRetry(failure(operation, kind, message), delay);
}
function failure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'gusto', operation, message };
}
