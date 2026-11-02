// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { newFence, CheckpointError, InsufficientTimeError } from '@superdurable/connectors-sdk/singledispatch';
import type { BambooHRClient, BambooHRExchange } from './client.js';
import { AddEmployeeDefinition, FindEmployeeByEmailDefinition, GetEmployeeDefinition, ListEmployeeChangesDefinition, ListTimeOffRequestsDefinition, UpdateEmployeeDefinition } from './generated/definitions.js';
import type { AddEmployeeInput, AddEmployeeOutput, EmployeeChange, EmployeeRecord, EmployeeSummary, FindEmployeeByEmailInput, FindEmployeeByEmailOutput, GetEmployeeInput, ListEmployeeChangesInput, ListEmployeeChangesOutput, ListTimeOffRequestsInput, ListTimeOffRequestsOutput, TimeOffRequest, UpdateEmployeeInput, UpdateEmployeeOutput } from './generated/models.js';

const employeeIdPattern = /^[1-9][0-9]{0,18}$/;
const fieldNamePattern = /^[A-Za-z][A-Za-z0-9_]{0,63}$/;
const fieldIdPattern = /^[1-9][0-9]{0,9}$/;
const zeroRecord: EmployeeRecord = { employeeId: '', fields: {} };
const zeroFind: FindEmployeeByEmailOutput = { employee: { employeeId: '' }, candidateCount: 0 };
const zeroChanges: ListEmployeeChangesOutput = { changes: [], nextCursor: { since: '0001-01-01T00:00:00Z' } };
const zeroTimeOff: ListTimeOffRequestsOutput = { requests: [], page: 0, totalItems: 0 };
const zeroUpdate: UpdateEmployeeOutput = { employee: zeroRecord };
const zeroAdd: AddEmployeeOutput = { employeeId: '' };
const summaryFields = new Set(['employeeId', 'firstName', 'lastName', 'preferredName', 'jobTitleName', 'status', 'workEmail', 'homeEmail', 'photoUrl', '_restrictedFields']);
const historyFields = new Set(['jobTitle', 'department', 'division', 'location', 'reportsTo', 'payRate', 'payType', 'payPer', 'paidPer', 'paySchedule', 'overtimeRate', 'exempt', 'employmentHistoryStatus', 'employmentStatus', 'employeeStatusDate', 'employmentType', 'terminationDate']);
const ignoredFields = new Set(['id', 'employeeId', 'photo', 'photoUrl']);

/** Read one employee with an explicit bounded field list. */
export class GetEmployeeOperation implements Query<GetEmployeeInput, EmployeeRecord> {
  /** Bind a client. */ constructor(private readonly client: BambooHRClient) {}
  /** Return the manifest definition. */ definition() { return GetEmployeeDefinition; }
  /** Read and classify one employee attempt. */
  async invoke(call: Call, input: GetEmployeeInput): Promise<QueryAttempt<EmployeeRecord>> {
    try { requireEmployeeId(input.employeeId); validateRequestedFields(input.fields, 1, 400); }
    catch { return defectQuery('getEmployee', zeroRecord); }
    const key = await this.client.resolveKey(call);
    if (!key) return defectQuery('getEmployee', zeroRecord, 'AUTHENTICATION');
    const query = new URLSearchParams({ fields: input.fields!.join(',') });
    if (input.includeFutureValues) query.set('onlyCurrent', 'false');
    const result = await this.client.exchange(call, 'getEmployee', key, 'GET', `/employees/${input.employeeId}`, query);
    const branch = exchangeQuery(this.client, call, result, zeroRecord, input.employeeId, true);
    if (branch) return branch;
    try { return newQueryBranch('found', decodeEmployeeRecord(parseBody(result), input.employeeId, input.fields!), this.client.receipt(call, input.employeeId)); }
    catch { return invalidQuery(this.client, call, 'getEmployee', zeroRecord, input.employeeId); }
  }
}

/** Find an exact email match from BambooHR's bounded substring search. */
export class FindEmployeeByEmailOperation implements Query<FindEmployeeByEmailInput, FindEmployeeByEmailOutput> {
  /** Bind a client. */ constructor(private readonly client: BambooHRClient) {}
  /** Return the manifest definition. */ definition() { return FindEmployeeByEmailDefinition; }
  /** Search one bounded page and classify its exact matches. */
  async invoke(call: Call, input: FindEmployeeByEmailInput): Promise<QueryAttempt<FindEmployeeByEmailOutput>> {
    const emailField = input.emailField || 'workEmail';
    if (!isBareEmail(input.email) || !['workEmail', 'homeEmail'].includes(emailField) || (input.status && !['active', 'inactive'].includes(input.status)) || (input.fields?.length ?? 0) > 20 || input.fields?.some(field => !fieldNamePattern.test(field))) return defectQuery('findEmployeeByEmail', zeroFind);
    const key = await this.client.resolveKey(call);
    if (!key) return defectQuery('findEmployeeByEmail', zeroFind, 'AUTHENTICATION');
    const fields = ['workEmail', 'homeEmail', ...(input.fields ?? []).filter(field => !summaryFields.has(field) && !['workEmail', 'homeEmail'].includes(field))];
    const query = new URLSearchParams({ [`filter[${emailField}]`]: input.email, fields: fields.join(','), sort: 'employeeId', 'page[limit]': '100' });
    if (input.status) query.set('filter[status]', input.status);
    const result = await this.client.exchange(call, 'findEmployeeByEmail', key, 'GET', '/employees', query);
    const branch = exchangeQuery(this.client, call, result, zeroFind);
    if (branch) return branch;
    try {
      const page = asObject(parseBody(result));
      const data = page.data;
      const meta = asObject(page.meta);
      if (!Array.isArray(data) || !Number.isInteger(meta.total) || (meta.total as number) < data.length || data.length > 100) throw new Error('invalid employee page');
      const candidates = data.map(item => decodeSummary(item, input.fields ?? []));
      const matches = candidates.filter(item => (emailField === 'homeEmail' ? item.homeEmail : item.workEmail)?.trim().toLowerCase() === input.email.toLowerCase());
      const pageMeta = meta.page && typeof meta.page === 'object' ? meta.page as Record<string, unknown> : {};
      const links = page._links && typeof page._links === 'object' ? page._links as Record<string, unknown> : {};
      const hasMore = Boolean(pageMeta.nextCursor || links.next) || (meta.total as number) > data.length;
      const output: FindEmployeeByEmailOutput = { employee: { employeeId: '' }, candidateCount: meta.total as number };
      if (matches.length === 1 && !hasMore) return newQueryBranch('found', { ...output, employee: matches[0]! }, this.client.receipt(call, matches[0]!.employeeId));
      if (matches.length === 0 && !hasMore) return newQueryBranch('notFound', output, this.client.receipt(call), failure('findEmployeeByEmail', 'NOT_FOUND', `no employee visible to the connection has this ${emailField}`));
      return newQueryBranch('ambiguous', { ...output, matches }, this.client.receipt(call), failure('findEmployeeByEmail', 'CONFLICT', `exact matches: ${matches.length} of ${meta.total} employees whose ${emailField} contains the address; no single employee was chosen`));
    } catch { return invalidQuery(this.client, call, 'findEmployeeByEmail', zeroFind); }
  }
}

/** Poll and page employee changes after a stable cursor. */
export class ListEmployeeChangesOperation implements Query<ListEmployeeChangesInput, ListEmployeeChangesOutput> {
  /** Bind a client. */ constructor(private readonly client: BambooHRClient) {}
  /** Return the manifest definition. */ definition() { return ListEmployeeChangesDefinition; }
  /** Poll and classify one change list attempt. */
  async invoke(call: Call, input: ListEmployeeChangesInput): Promise<QueryAttempt<ListEmployeeChangesOutput>> {
    const limit = input.limit || 100;
    const sinceMs = Date.parse(input.since);
    if (!Number.isFinite(sinceMs) || sinceMs <= 0 || (input.afterEmployeeId && !employeeIdPattern.test(input.afterEmployeeId)) || (input.changeType && !['inserted', 'updated', 'deleted'].includes(input.changeType)) || !Number.isInteger(limit) || limit < 1 || limit > 1000) return defectQuery('listEmployeeChanges', zeroChanges);
    const key = await this.client.resolveKey(call);
    if (!key) return defectQuery('listEmployeeChanges', zeroChanges, 'AUTHENTICATION');
    const query = new URLSearchParams({ since: isoSeconds(sinceMs - 1000) });
    if (input.changeType) query.set('type', input.changeType);
    const result = await this.client.exchange(call, 'listEmployeeChanges', key, 'GET', '/employees/changed', query);
    const branch = exchangeQuery(this.client, call, result, zeroChanges);
    if (branch) return branch;
    try {
      const document = asObject(parseBody(result));
      const wire = document.employees;
      if (wire === null || wire === undefined || (typeof wire !== 'object')) throw new Error('invalid change list');
      const entries = Array.isArray(wire) ? wire.length === 0 ? [] : (() => { throw new Error('invalid change list'); })() : Object.entries(asObject(wire));
      const changes: EmployeeChange[] = entries.map(([employeeId, raw]) => {
        requireEmployeeId(employeeId);
        const entry = asObject(raw);
        if (entry.id !== undefined && String(entry.id) !== employeeId) throw new Error('employee ID mismatch');
        if (typeof entry.action !== 'string' || !/^[A-Za-z]{1,32}$/.test(entry.action) || typeof entry.lastChanged !== 'string' || !Number.isFinite(Date.parse(entry.lastChanged))) throw new Error('invalid change');
        return { employeeId, action: entry.action, lastChanged: isoSeconds(Date.parse(entry.lastChanged)) };
      }).filter(change => Date.parse(change.lastChanged) > sinceMs || (Date.parse(change.lastChanged) === sinceMs && (!input.afterEmployeeId || compareIds(change.employeeId, input.afterEmployeeId) > 0)));
      changes.sort((a, b) => Date.parse(a.lastChanged) - Date.parse(b.lastChanged) || compareIds(a.employeeId, b.employeeId));
      const selected = changes.slice(0, limit);
      const last = selected.at(-1);
      const output: ListEmployeeChangesOutput = { changes: selected, nextCursor: last ? { since: last.lastChanged, afterEmployeeId: last.employeeId } : { since: isoSeconds(sinceMs), ...(input.afterEmployeeId ? { afterEmployeeId: input.afterEmployeeId } : {}) }, ...(changes.length > limit ? { hasMore: true } : {}) };
      return newQueryBranch('listed', output, this.client.receipt(call));
    } catch { return invalidQuery(this.client, call, 'listEmployeeChanges', zeroChanges); }
  }
}

/** Read a bounded page of time off requests. */
export class ListTimeOffRequestsOperation implements Query<ListTimeOffRequestsInput, ListTimeOffRequestsOutput> {
  /** Bind a client. */ constructor(private readonly client: BambooHRClient) {}
  /** Return the manifest definition. */ definition() { return ListTimeOffRequestsDefinition; }
  /** Read and classify one page. */
  async invoke(call: Call, input: ListTimeOffRequestsInput): Promise<QueryAttempt<ListTimeOffRequestsOutput>> {
    const page = input.page || 1;
    const pageSize = input.pageSize || 100;
    if (!isCivilDate(input.startDate) || !isCivilDate(input.endDate) || input.endDate < input.startDate || (input.employeeId && !employeeIdPattern.test(input.employeeId)) || input.statuses?.some(status => !['REQUESTED', 'APPROVED', 'DENIED', 'CANCELED'].includes(status)) || !Number.isInteger(page) || page < 1 || page > 2147483647 || !Number.isInteger(pageSize) || pageSize < 1 || pageSize > 200) return defectQuery('listTimeOffRequests', zeroTimeOff);
    const key = await this.client.resolveKey(call);
    if (!key) return defectQuery('listTimeOffRequests', zeroTimeOff, 'AUTHENTICATION');
    const query = new URLSearchParams({ filter: buildTimeOffRequestFilter(input), page: String(page), pageSize: String(pageSize) });
    const result = await this.client.exchange(call, 'listTimeOffRequests', key, 'GET', '/time-off/requests', query);
    const branch = exchangeQuery(this.client, call, result, zeroTimeOff, input.employeeId ?? '');
    if (branch) return branch;
    try {
      const document = asObject(parseBody(result));
      const meta = asObject(document.meta);
      if (!Array.isArray(document.data) || document.data.length > pageSize || !Number.isInteger(meta.totalItems) || (meta.totalItems as number) < 0 || !Number.isInteger(meta.totalPages) || (meta.totalPages as number) < 0) throw new Error('invalid time off page');
      const requests = document.data.map(decodeTimeOffRequest);
      return newQueryBranch('listed', { requests, page, totalItems: meta.totalItems as number, ...(page < (meta.totalPages as number) ? { nextPage: page + 1 } : {}) }, this.client.receipt(call, input.employeeId ?? ''));
    } catch { return invalidQuery(this.client, call, 'listTimeOffRequests', zeroTimeOff, input.employeeId ?? ''); }
  }
}

/** Build the OData filter sent by listTimeOffRequests. */
export function buildTimeOffRequestFilter(input: ListTimeOffRequestsInput): string {
  const clauses = [`startDate le '${input.endDate}'`, `endDate ge '${input.startDate}'`];
  if (input.employeeId) clauses.push(`employeeId eq ${input.employeeId}`);
  if (input.statuses?.length === 1) clauses.push(`status eq '${input.statuses[0]!.toLowerCase()}'`);
  if (input.statuses && input.statuses.length > 1) clauses.push(`status in (${input.statuses.map(status => `'${status.toLowerCase()}'`).join(', ')})`);
  return clauses.join(' and ');
}

/** Set absolute employee field values after reading the current values. */
export class UpdateEmployeeOperation implements Mutation<UpdateEmployeeInput, UpdateEmployeeOutput> {
  /** Bind a client. */ constructor(private readonly client: BambooHRClient) {}
  /** Return the manifest definition. */ definition() { return UpdateEmployeeDefinition; }
  /** Correlate one repeat-safe write with the Dex Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Read, write only changed fields, and read back. */
  async invoke(call: Call, input: UpdateEmployeeInput): Promise<MutationAttempt<UpdateEmployeeOutput>> {
    try { requireEmployeeId(input.employeeId); validateWrittenFields(input.fields, 1, new Set([...historyFields, ...ignoredFields])); }
    catch { return defectMutation('updateEmployee', zeroUpdate); }
    const key = await this.client.resolveKey(call);
    if (!key) return defectMutation('updateEmployee', zeroUpdate, 'AUTHENTICATION');
    const fieldNames = Object.keys(input.fields!).sort();
    const receipt = this.client.receipt(call, input.employeeId);
    const read = async (value: UpdateEmployeeOutput): Promise<EmployeeRecord | MutationAttempt<UpdateEmployeeOutput>> => {
      const result = await this.client.exchange(call, 'updateEmployee', key, 'GET', `/employees/${input.employeeId}`, new URLSearchParams({ fields: fieldNames.join(',') }));
      const terminal = exchangeMutation(this.client, call, result, value, input.employeeId, true);
      if (terminal) return terminal;
      try { return decodeEmployeeRecord(parseBody(result), input.employeeId, fieldNames); }
      catch { return newMutationBranch('invalidResponse', value, receipt, failure('updateEmployee', 'PROTOCOL', 'BambooHR returned an invalid employee')); }
    };
    const current = await read(zeroUpdate);
    if (!('employeeId' in current)) return current;
    const changed: Record<string, string> = {};
    for (const [field, text] of Object.entries(input.fields!)) if (current.fields?.[field] !== text) changed[field] = text;
    if (Object.keys(changed).length === 0) return newMutationBranch('updated', { employee: current, wasAlreadyApplied: true }, receipt);
    const write = await this.client.exchange(call, 'updateEmployee', key, 'POST', `/employees/${input.employeeId}`, undefined, changed);
    const writeTerminal = exchangeMutation(this.client, call, write, { employee: current }, input.employeeId, true);
    if (writeTerminal) return writeTerminal;
    const writtenFields = Object.keys(changed).sort();
    const output: UpdateEmployeeOutput = { employee: zeroRecord, writtenFields };
    const stored = await read(output);
    if (!('employeeId' in stored)) return stored;
    const unmatchedFields = fieldNames.filter(field => stored.fields?.[field] !== input.fields![field]);
    return newMutationBranch('updated', { employee: stored, writtenFields, ...(unmatchedFields.length ? { unmatchedFields } : {}) }, receipt);
  }
}

/** Add an employee once under the SDK's durable single-dispatch fence. */
export class AddEmployeeOperation implements Mutation<AddEmployeeInput, AddEmployeeOutput> {
  /** Bind a client. */ constructor(private readonly client: BambooHRClient) {}
  /** Return the manifest definition. */ definition() { return AddEmployeeDefinition; }
  /** Correlate the fence and receipt with the Dex Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Fence the write, checkpoint safe retries, and read back known IDs. */
  async invoke(call: Call, input: AddEmployeeInput): Promise<MutationAttempt<AddEmployeeOutput>> {
    if (!validPersonName(input.firstName, true) || !validPersonName(input.lastName, true) || !validPersonName(input.preferredName ?? '', false) || (input.workEmail && !isBareEmail(input.workEmail)) || (input.homeEmail && !isBareEmail(input.homeEmail)) || (input.hireDate && !isCivilDate(input.hireDate))) return defectMutation('addEmployee', zeroAdd);
    try { validateWrittenFields(input.fields ?? {}, 0, new Set([...ignoredFields, 'firstName', 'lastName', 'preferredName', 'workEmail', 'homeEmail', 'hireDate'])); }
    catch { return defectMutation('addEmployee', zeroAdd); }
    const receipt = this.client.receipt(call);
    const fence = newFence(call, AddEmployeeDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000);
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return newMutationUncertain(zeroAdd, failure('addEmployee', 'TRANSPORT', 'the dispatch checkpoint is unavailable or untrusted; the request is not sent again'), receipt);
    if (decision.kind === 'readBackRequired') {
      const employeeId = decision.marker?.providerId;
      if (!employeeId || decision.marker?.callId !== call.id || !employeeIdPattern.test(employeeId)) return newMutationUncertain(zeroAdd, failure('addEmployee', 'TRANSPORT', 'an earlier attempt may have sent the request; no employee ID is available for read-back'), receipt);
      const key = await this.client.resolveKey(call);
      if (!key) return newMutationRetry(failure('addEmployee', 'AUTHENTICATION', 'connection credentials are unavailable'));
      const result = await this.client.exchange(call, 'addEmployee', key, 'GET', `/employees/${employeeId}`);
      if (result.kind === 'success') {
        try { if (decodeEmployeeId(asObject(parseBody(result)).id) === employeeId) return newMutationBranch('created', { employeeId }, this.client.receipt(call, employeeId)); }
        catch { /* A previously accepted write cannot be safely repeated. */ }
        return newMutationUncertain(zeroAdd, failure('addEmployee', 'PROTOCOL', 'the created employee ID could not be confirmed'), this.client.receipt(call, employeeId));
      }
      if (result.kind === 'notFound' || result.kind === 'rejected') return newMutationUncertain(zeroAdd, result.failure, this.client.receipt(call, employeeId));
      return newMutationRetry(result.failure, result.retryAfterMs);
    }
    const key = await this.client.resolveKey(call);
    if (!key) {
      try { await fence.recordNotSent(); }
      catch { return newMutationRetry(failure('addEmployee', 'AVAILABILITY', 'credentials and not-sent checkpoint are unavailable')); }
      return defectMutation('addEmployee', zeroAdd, 'AUTHENTICATION');
    }
    const payload: Record<string, string> = { ...(input.fields ?? {}), firstName: input.firstName, lastName: input.lastName };
    for (const field of ['preferredName', 'workEmail', 'homeEmail', 'hireDate'] as const) if (input[field]) payload[field] = input[field]!;
    let result: BambooHRExchange;
    try { result = await fence.dispatch(signal => this.client.exchange(call, 'addEmployee', key, 'POST', '/employees', undefined, payload, signal)); }
    catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError) return newMutationRetry(failure('addEmployee', 'AVAILABILITY', 'dispatch checkpoint or attempt time is unavailable; nothing was sent'));
      return newMutationUncertain(zeroAdd, failure('addEmployee', 'TRANSPORT', 'the dispatch outcome is unavailable'), receipt);
    }
    if (result.kind === 'success') {
      let employeeId = '';
      try { employeeId = decodeEmployeeId(asObject(parseBody(result)).id); }
      catch {
        const location = result.response.headers.location;
        const match = typeof location === 'string' ? /(?:\/employees\/|[?&]id=)([1-9][0-9]{0,18})(?:[/?&#]|$)/.exec(location) : null;
        employeeId = match?.[1] ?? '';
      }
      if (!employeeId) return newMutationUncertain(zeroAdd, failure('addEmployee', 'PROTOCOL', 'BambooHR accepted the employee but returned no usable employee ID'), receipt);
      try { await fence.recordProviderId(employeeId); }
      catch { /* A failed identity checkpoint cannot authorize another POST. */ }
      return newMutationBranch('created', { employeeId }, this.client.receipt(call, employeeId));
    }
    if (result.kind === 'rateLimited' || (result.kind === 'unavailable' && result.failure.kind === 'AVAILABILITY' && result.failure.message.includes('HTTP 503')) || result.kind === 'notSent') {
      try { await fence.recordCanResend(); }
      catch { return newMutationRetry(failure('addEmployee', 'AVAILABILITY', 'BambooHR did not process the request, but the resend checkpoint failed'), result.retryAfterMs); }
      return newMutationRetry(result.failure, result.retryAfterMs);
    }
    if (result.kind === 'unavailable' || result.kind === 'invalid') return newMutationUncertain(zeroAdd, result.failure, receipt);
    if (result.kind === 'defect') return newMutationBranch('defect', zeroAdd, receipt, result.failure);
    return newMutationBranch('providerRejected', zeroAdd, receipt, result.failure);
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'bamboohr', operation, message }; }
function defectQuery<Output>(operation: string, value: Output, kind: Failure['kind'] = 'VALIDATION'): QueryAttempt<Output> { return newQueryBranch('defect', value, {}, failure(operation, kind, kind === 'AUTHENTICATION' ? 'connection credentials are unavailable' : 'invalid input')); }
function defectMutation<Output>(operation: string, value: Output, kind: Failure['kind'] = 'VALIDATION'): MutationAttempt<Output> { return newMutationBranch('defect', value, {}, failure(operation, kind, kind === 'AUTHENTICATION' ? 'connection credentials are unavailable' : 'invalid input')); }
function invalidQuery<Output>(client: BambooHRClient, call: Call, operation: string, value: Output, objectId = ''): QueryAttempt<Output> { return newQueryBranch('invalidResponse', value, client.receipt(call, objectId), failure(operation, 'PROTOCOL', 'BambooHR returned an invalid response')); }
function exchangeQuery<Output>(client: BambooHRClient, call: Call, result: BambooHRExchange, value: Output, objectId = '', hasNotFound = false): QueryAttempt<Output> | undefined {
  if (result.kind === 'success') return undefined;
  if (['rateLimited', 'notSent', 'unavailable'].includes(result.kind)) return newQueryRetry(result.failure, result.retryAfterMs);
  const branch = result.kind === 'invalid' ? 'invalidResponse' : result.kind === 'defect' ? 'defect' : result.kind === 'notFound' && hasNotFound ? 'notFound' : 'providerRejected';
  return newQueryBranch(branch, value, client.receipt(call, objectId), result.failure);
}
function exchangeMutation<Output>(client: BambooHRClient, call: Call, result: BambooHRExchange, value: Output, objectId = '', hasNotFound = false): MutationAttempt<Output> | undefined {
  if (result.kind === 'success') return undefined;
  if (['rateLimited', 'notSent', 'unavailable'].includes(result.kind)) return newMutationRetry(result.failure, result.retryAfterMs);
  const branch = result.kind === 'invalid' ? 'invalidResponse' : result.kind === 'defect' ? 'defect' : result.kind === 'notFound' && hasNotFound ? 'notFound' : 'providerRejected';
  return newMutationBranch(branch, value, client.receipt(call, objectId), result.failure);
}
function parseBody(result: BambooHRExchange): unknown { if (result.kind !== 'success') throw new Error('response unavailable'); return JSON.parse(Buffer.from(result.response.body).toString('utf8')) as unknown; }
function asObject(value: unknown): Record<string, unknown> { if (value === null || typeof value !== 'object' || Array.isArray(value)) throw new Error('expected object'); return value as Record<string, unknown>; }
function decodeEmployeeId(value: unknown): string { const id = typeof value === 'number' ? String(value) : value; if (typeof id !== 'string' || !employeeIdPattern.test(id)) throw new Error('invalid employee ID'); return id; }
function requireEmployeeId(value: string): void { if (!employeeIdPattern.test(value)) throw new Error('invalid employee ID'); }
function textValue(value: unknown): string { if (value === null) return ''; if (typeof value === 'string') return value; if (typeof value === 'number' || typeof value === 'boolean') return String(value); return JSON.stringify(value); }
function decodeEmployeeRecord(value: unknown, employeeId: string, requested: string[]): EmployeeRecord {
  const document = asObject(value);
  if (decodeEmployeeId(document.id) !== employeeId) throw new Error('employee ID mismatch');
  const fields: Record<string, string> = {};
  for (const [name, field] of Object.entries(document)) if (name !== 'id') fields[name] = textValue(field);
  const omittedFields = requested.filter(field => !(field in fields)).sort();
  return { employeeId, fields, ...(omittedFields.length ? { omittedFields } : {}) };
}
function decodeSummary(value: unknown, requested: string[]): EmployeeSummary {
  const document = asObject(value);
  const employeeId = decodeEmployeeId(document.employeeId);
  const summary: EmployeeSummary = { employeeId };
  for (const name of ['firstName', 'lastName', 'preferredName', 'jobTitleName', 'status', 'workEmail', 'homeEmail'] as const) {
    if (name in document && textValue(document[name])) summary[name] = textValue(document[name]);
  }
  const fields: Record<string, string> = {};
  for (const field of requested) if (!summaryFields.has(field) && field in document) fields[field] = textValue(document[field]);
  if (Object.keys(fields).length) summary.fields = fields;
  if (document._restrictedFields !== undefined) {
    if (!Array.isArray(document._restrictedFields) || !document._restrictedFields.every(field => typeof field === 'string')) throw new Error('invalid restricted fields');
    if (document._restrictedFields.length) summary.restrictedFields = document._restrictedFields as string[];
  }
  return summary;
}
function decodeTimeOffRequest(value: unknown): TimeOffRequest {
  const document = asObject(value);
  if (!Number.isSafeInteger(document.id) || (document.id as number) < 1 || !Number.isSafeInteger(document.employeeId) || (document.employeeId as number) < 1 || !isCivilDate(document.startDate) || !isCivilDate(document.endDate)) throw new Error('invalid time off request');
  const unit = String(document.unit).toUpperCase();
  const status = String(document.status).toUpperCase();
  if (!['HOURS', 'DAYS'].includes(unit) || !['REQUESTED', 'APPROVED', 'DENIED', 'CANCELED'].includes(status)) throw new Error('invalid time off status');
  const requestedAt = parseTime(document.requestedAt);
  const output: TimeOffRequest = { id: document.id as number, employeeId: String(document.employeeId), categoryId: typeof document.categoryId === 'number' ? document.categoryId : 0, startDate: document.startDate as string, endDate: document.endDate as string, amount: typeof document.amount === 'number' ? document.amount : 0, unit, status, requestedAt };
  if (document.statusUpdatedAt !== undefined && document.statusUpdatedAt !== null) output.statusUpdatedAt = parseTime(document.statusUpdatedAt);
  if (Array.isArray(document.dailyAmounts) && document.dailyAmounts.length) output.dailyAmounts = document.dailyAmounts.map(item => { const daily = asObject(item); if (!isCivilDate(daily.date) || typeof daily.amount !== 'number') throw new Error('invalid daily amount'); return { date: daily.date, amount: daily.amount }; });
  return output;
}
function parseTime(value: unknown): string { if (typeof value !== 'string' || !Number.isFinite(Date.parse(value))) return '0001-01-01T00:00:00Z'; return isoSeconds(Date.parse(value)); }
function isoSeconds(milliseconds: number): string { return new Date(milliseconds).toISOString().replace(/\.000Z$/, 'Z'); }
function compareIds(left: string, right: string): number { const a = BigInt(left); const b = BigInt(right); return a < b ? -1 : a > b ? 1 : 0; }
function validateRequestedFields(fields: string[] | null, minimum: number, maximum: number): void {
  if (!fields || fields.length < minimum || fields.length > maximum || fields.some(field => !fieldNamePattern.test(field) && !fieldIdPattern.test(field)) || new Set(fields).size !== fields.length) throw new Error('invalid requested fields');
}
function validateWrittenFields(fields: Record<string, string> | null, minimum: number, refused: Set<string>): void {
  if (!fields || Object.keys(fields).length < minimum || Object.keys(fields).length > 50) throw new Error('invalid written fields');
  const refusedLower = new Set([...refused].map(field => field.toLowerCase()));
  for (const [name, value] of Object.entries(fields)) if (!fieldNamePattern.test(name) || refusedLower.has(name.toLowerCase()) || typeof value !== 'string' || Buffer.byteLength(value) > 4096 || value.includes('\0')) throw new Error('invalid written field');
}
function isBareEmail(value: string): boolean { return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value); }
function isCivilDate(value: unknown): value is string { return typeof value === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(value) && !Number.isNaN(Date.parse(`${value}T00:00:00Z`)) && new Date(`${value}T00:00:00Z`).toISOString().slice(0, 10) === value; }
function validPersonName(value: string, required: boolean): boolean { return (!required || value.length > 0) && (value.length === 0 || (Buffer.byteLength(value) <= 255 && value.trim() === value && ![...value].some(character => { const code = character.codePointAt(0)!; return code < 32 || code === 127; }))); }
