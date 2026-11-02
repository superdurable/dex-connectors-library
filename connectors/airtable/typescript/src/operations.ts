// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { AirtableClient, ExchangeFailure } from './client.js';
import { GetRecordDefinition, ListRecordsDefinition, UpdateRecordsDefinition, UpsertRecordsDefinition } from './generated/definitions.js';
import type { FieldEqualityFilter, GetRecordInput, ListRecordsInput, Record as AirtableRecord, RecordPage, UpdatedRecords, UpdateRecordsInput, UpsertedRecords, UpsertRecordsInput } from './generated/models.js';

const baseIdPattern = /^app[0-9A-Za-z]{14}$/;
const recordIdPattern = /^rec[0-9A-Za-z]{14}$/;
const zeroTime = '0001-01-01T00:00:00Z';
const zeroRecord: AirtableRecord = { id: '', createdTime: zeroTime, fields: {} };
const zeroPage: RecordPage = { records: [] };
const zeroUpserted: UpsertedRecords = { records: [], createdRecordIds: [], updatedRecordIds: [] };
const zeroUpdated: UpdatedRecords = { records: [] };

/** Query one bounded Airtable record page. */
export class ListRecordsOperation implements Query<ListRecordsInput, RecordPage> {
  /** Bind one authenticated client. */ constructor(private readonly client: AirtableClient) {}
  /** Return the manifest definition. */ definition() { return ListRecordsDefinition; }
  /** Validate, send, and classify one list attempt. */
  async invoke(call: Call, input: ListRecordsInput): Promise<QueryAttempt<RecordPage>> {
    let requestBody: Record<string, unknown>;
    try { requestBody = buildListRequest(input); }
    catch (error) { return queryDefect('listRecords', zeroPage, error); }
    const objectId = `${input.baseId}/${input.tableIdOrName}`;
    const result = await this.client.exchange(call, 'listRecords', input.baseId, objectId, 'POST', `${recordPath(input)}/listRecords`, requestBody);
    if ('outcome' in result) return queryOutcome(result.outcome, zeroPage, 'providerRejected');
    try {
      const body = decodeObject(result.response.body);
      if (!Array.isArray(body.records) || body.records.length > Number(requestBody.pageSize) || !body.records.every(isObject)) throw new Error('Airtable returned an invalid record page');
      const offset = body.offset === undefined ? '' : requireString(body.offset);
      if (Buffer.byteLength(offset, 'utf8') > 1024) throw new Error('Airtable returned an oversized offset');
      const records = body.records.map(decodeRecord);
      const output: RecordPage = { records, ...(offset ? { offset } : {}), ...(requestBody.filterByFormula ? { filterFormula: String(requestBody.filterByFormula) } : {}) };
      return newQueryBranch('listed', output, this.client.receipt(call, objectId));
    } catch { return queryFailure('listRecords', 'invalidResponse', 'PROTOCOL', zeroPage, this.client.receipt(call, objectId)); }
  }
}

/** Query one Airtable record by ID. */
export class GetRecordOperation implements Query<GetRecordInput, AirtableRecord> {
  /** Bind one authenticated client. */ constructor(private readonly client: AirtableClient) {}
  /** Return the manifest definition. */ definition() { return GetRecordDefinition; }
  /** Validate, send, and classify one record read. */
  async invoke(call: Call, input: GetRecordInput): Promise<QueryAttempt<AirtableRecord>> {
    try { validateBaseAndTable(input.baseId, input.tableIdOrName); if (!recordIdPattern.test(input.recordId)) throw new Error('recordId must be an Airtable record ID'); }
    catch (error) { return queryDefect('getRecord', zeroRecord, error); }
    const result = await this.client.exchange(call, 'getRecord', input.baseId, input.recordId, 'GET', `${recordPath(input)}/${encodeURIComponent(input.recordId)}`);
    if ('outcome' in result) return queryOutcome(result.outcome, zeroRecord, 'providerRejected', 'notFound');
    try {
      const record = decodeRecord(decodeObject(result.response.body));
      if (record.id !== input.recordId) throw new Error('record ID mismatch');
      return newQueryBranch('found', record, this.client.receipt(call, input.recordId));
    } catch { return queryFailure('getRecord', 'invalidResponse', 'PROTOCOL', zeroRecord, this.client.receipt(call, input.recordId)); }
  }
}

/** Mutation that replaces records by stable merge fields. */
export class UpsertRecordsOperation implements Mutation<UpsertRecordsInput, UpsertedRecords> {
  /** Bind one authenticated client. */ constructor(private readonly client: AirtableClient) {}
  /** Return the manifest definition. */ definition() { return UpsertRecordsDefinition; }
  /** Reuse the Dex Call ID; Airtable does not accept an idempotency header. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate and send one repeat-safe merge-field upsert. */
  async invoke(call: Call, input: UpsertRecordsInput): Promise<MutationAttempt<UpsertedRecords>> {
    let requestBody: Record<string, unknown>;
    try { requestBody = buildUpsertRequest(input); }
    catch (error) { return mutationDefect('upsertRecords', zeroUpserted, error); }
    const objectId = `${input.baseId}/${input.tableIdOrName}`;
    const result = await this.client.exchange(call, 'upsertRecords', input.baseId, objectId, 'PATCH', recordPath(input), requestBody);
    if ('outcome' in result) return mutationOutcome(result.outcome, zeroUpserted, 'providerRejected');
    try {
      const body = decodeObject(result.response.body);
      if (!Array.isArray(body.records) || body.records.length !== input.records?.length || !Array.isArray(body.createdRecords) || !Array.isArray(body.updatedRecords)) throw new Error('invalid upsert response');
      const records = body.records.map(decodeRecord);
      const createdRecordIds = body.createdRecords.map(requireString);
      const updatedRecordIds = body.updatedRecords.map(requireString);
      const writtenIds = new Set(records.map(record => record.id));
      if ([...createdRecordIds, ...updatedRecordIds].some(id => !writtenIds.has(id)) || createdRecordIds.some(id => updatedRecordIds.includes(id))) throw new Error('invalid upsert IDs');
      const partialSuccessReasons = decodePartialSuccessReasons(body.details);
      return newMutationBranch('upserted', { records, createdRecordIds, updatedRecordIds, ...(partialSuccessReasons.length ? { partialSuccessReasons } : {}) }, this.client.receipt(call, objectId));
    } catch { return mutationFailure('upsertRecords', 'invalidResponse', 'PROTOCOL', zeroUpserted, this.client.receipt(call, objectId)); }
  }
}

/** Mutation that sets absolute field values on named records. */
export class UpdateRecordsOperation implements Mutation<UpdateRecordsInput, UpdatedRecords> {
  /** Bind one authenticated client. */ constructor(private readonly client: AirtableClient) {}
  /** Return the manifest definition. */ definition() { return UpdateRecordsDefinition; }
  /** Reuse the Dex Call ID; absolute replacement makes retries safe. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate and send one repeat-safe update by record ID. */
  async invoke(call: Call, input: UpdateRecordsInput): Promise<MutationAttempt<UpdatedRecords>> {
    let requestBody: Record<string, unknown>;
    try { requestBody = buildUpdateRequest(input); }
    catch (error) { return mutationDefect('updateRecords', zeroUpdated, error); }
    const objectId = `${input.baseId}/${input.tableIdOrName}`;
    const result = await this.client.exchange(call, 'updateRecords', input.baseId, objectId, 'PATCH', recordPath(input), requestBody);
    if ('outcome' in result) return mutationOutcome(result.outcome, zeroUpdated, 'providerRejected', 'notFound');
    try {
      const body = decodeObject(result.response.body);
      if (!Array.isArray(body.records) || body.records.length !== input.records?.length) throw new Error('invalid update response');
      const records = body.records.map(decodeRecord);
      const requestedIds = new Set(input.records!.map(record => record.id));
      if (records.some(record => !requestedIds.has(record.id))) throw new Error('unexpected updated record');
      const partialSuccessReasons = decodePartialSuccessReasons(body.details);
      return newMutationBranch('updated', { records, ...(partialSuccessReasons.length ? { partialSuccessReasons } : {}) }, this.client.receipt(call, objectId));
    } catch { return mutationFailure('updateRecords', 'invalidResponse', 'PROTOCOL', zeroUpdated, this.client.receipt(call, objectId)); }
  }
}

function queryOutcome<T>(outcome: ExchangeFailure, zero: T, rejectedBranch: string, notFoundBranch = rejectedBranch): QueryAttempt<T> {
  if (outcome.disposition === 'retry') return newQueryRetry(outcome.failure, outcome.retryAfterMs);
  const branch = outcome.disposition === 'notFound' ? notFoundBranch : outcome.disposition === 'offsetExpired' ? 'offsetExpired' : outcome.disposition === 'rejected' ? rejectedBranch : outcome.disposition;
  return newQueryBranch(branch, zero, outcome.receipt, outcome.failure);
}

function mutationOutcome<T>(outcome: ExchangeFailure, zero: T, rejectedBranch: string, notFoundBranch = rejectedBranch): MutationAttempt<T> {
  if (outcome.disposition === 'retry') return newMutationRetry(outcome.failure, outcome.retryAfterMs);
  const branch = outcome.disposition === 'notFound' ? notFoundBranch : outcome.disposition === 'rejected' || outcome.disposition === 'offsetExpired' ? rejectedBranch : outcome.disposition;
  return newMutationBranch(branch, zero, outcome.receipt, outcome.failure);
}

function queryDefect<T>(operation: string, zero: T, error: unknown): QueryAttempt<T> { return queryFailure(operation, 'defect', 'VALIDATION', zero, {}, error); }
function mutationDefect<T>(operation: string, zero: T, error: unknown): MutationAttempt<T> { return mutationFailure(operation, 'defect', 'VALIDATION', zero, {}, error); }
function queryFailure<T>(operation: string, branch: string, kind: Failure['kind'], zero: T, receipt: ReceiptDraft, error?: unknown): QueryAttempt<T> {
  return newQueryBranch(branch, zero, receipt, { kind, provider: 'airtable', operation, message: error instanceof Error ? error.message : 'Airtable response is invalid' });
}
function mutationFailure<T>(operation: string, branch: string, kind: Failure['kind'], zero: T, receipt: ReceiptDraft, error?: unknown): MutationAttempt<T> {
  return newMutationBranch(branch, zero, receipt, { kind, provider: 'airtable', operation, message: error instanceof Error ? error.message : 'Airtable response is invalid' });
}

function validateIdentifier(label: string, value: string): void {
  if (!value) throw new Error(`${label} is required`);
  if (Buffer.byteLength(value, 'utf8') > 1_000 || /[\p{Cc}\p{Cf}]/u.test(value)) throw new Error(`${label} is invalid`);
}

function validateBaseAndTable(baseId: string, table: string): void {
  if (!baseIdPattern.test(baseId)) throw new Error('baseId must be an Airtable base ID');
  validateIdentifier('tableIdOrName', table);
}

function recordPath(input: { baseId: string; tableIdOrName: string }): string {
  return `/v0/${encodeURIComponent(input.baseId)}/${encodeURIComponent(input.tableIdOrName)}`;
}

function buildListRequest(input: ListRecordsInput): Record<string, unknown> {
  validateBaseAndTable(input.baseId, input.tableIdOrName);
  const pageSize = input.pageSize || 100;
  if (!Number.isInteger(pageSize) || pageSize < 1 || pageSize > 100) throw new Error('pageSize must be between 1 and 100');
  if (Buffer.byteLength(input.offset ?? '', 'utf8') > 1_024) throw new Error('offset is too long');
  const filterByFormula = buildFilterFormula(input.filters ?? [], input.formula ?? '');
  if ((input.fields?.length ?? 0) > 500) throw new Error('too many fields');
  const fields = uniqueIdentifiers('fields', input.fields ?? []);
  if ((input.sort?.length ?? 0) > 10) throw new Error('too many sort entries');
  input.sort?.forEach((sort, index) => { validateIdentifier(`sort[${index}].field`, sort.field); if (sort.direction && sort.direction !== 'asc' && sort.direction !== 'desc') throw new Error('sort direction is invalid'); });
  if (input.viewIdOrName) validateIdentifier('viewIdOrName', input.viewIdOrName);
  return {
    pageSize, ...(input.offset ? { offset: input.offset } : {}), ...(filterByFormula ? { filterByFormula } : {}),
    ...(fields.length ? { fields } : {}), ...(input.sort?.length ? { sort: input.sort.map(sort => ({ field: sort.field, ...(sort.direction ? { direction: sort.direction } : {}) })) } : {}),
    ...(input.viewIdOrName ? { view: input.viewIdOrName } : {}),
  };
}

function buildFilterFormula(filters: FieldEqualityFilter[], rawFormula: string): string {
  if (filters.length > 25 || Buffer.byteLength(rawFormula, 'utf8') > 16_000) throw new Error('filter formula is too long');
  const conditions = filters.map(filterCondition);
  if (rawFormula.trim()) conditions.push(`(${rawFormula})`);
  const formula = conditions.length === 0 ? '' : conditions.length === 1 ? conditions[0]! : `AND(${conditions.join(', ')})`;
  if (Buffer.byteLength(formula, 'utf8') > 16_000) throw new Error('filter formula is too long');
  return formula;
}

function filterCondition(filter: FieldEqualityFilter): string {
  validateIdentifier('field', filter.field);
  if (/[{}\\]/.test(filter.field)) throw new Error('field name cannot contain braces or a backslash');
  const selected = [filter.text !== undefined, filter.number !== undefined, filter.checkbox !== undefined].filter(Boolean).length;
  if (selected !== 1) throw new Error('exactly one filter value is required');
  const field = `{${filter.field}}`;
  if (filter.text !== undefined) {
    if (/[\\\p{Cc}\p{Cf}]/u.test(filter.text)) throw new Error('text filter is invalid');
    return `${field} = "${filter.text.replaceAll('"', '\\"')}"`;
  }
  if (filter.number !== undefined) {
    if (!Number.isFinite(filter.number)) throw new Error('number filter must be finite');
    return `${field} = ${filter.number}`;
  }
  return filter.checkbox ? `${field} = TRUE()` : `NOT(${field})`;
}

function uniqueIdentifiers(label: string, values: string[]): string[] {
  values.forEach((value, index) => validateIdentifier(`${label}[${index}]`, value));
  return [...new Set(values)];
}

function validateWrittenFields(label: string, fields: Record<string, unknown | null> | null): asserts fields is Record<string, unknown | null> {
  if (!fields || Object.keys(fields).length < 1 || Object.keys(fields).length > 500) throw new Error(`${label} needs 1 to 500 fields`);
  for (const key of Object.keys(fields)) {
    validateIdentifier('field name', key);
    if (fields[key] === undefined) throw new Error(`${label} has an undefined cell value`);
    try { JSON.stringify(fields[key]); }
    catch { throw new Error(`${label} has an invalid cell value`); }
  }
}

function buildUpsertRequest(input: UpsertRecordsInput): Record<string, unknown> {
  validateBaseAndTable(input.baseId, input.tableIdOrName);
  if (!input.fieldsToMergeOn || input.fieldsToMergeOn.length < 1 || input.fieldsToMergeOn.length > 3) throw new Error('fieldsToMergeOn must name 1 to 3 fields');
  const mergeFields = uniqueIdentifiers('fieldsToMergeOn', input.fieldsToMergeOn);
  if (mergeFields.length !== input.fieldsToMergeOn.length) throw new Error('fieldsToMergeOn cannot repeat a field');
  if (!input.records || input.records.length < 1 || input.records.length > 10) throw new Error('records must hold 1 to 10 records');
  const seen = new Set<string>();
  const records = input.records.map((record, index) => {
    const fields = record.fields;
    validateWrittenFields(`records[${index}]`, fields);
    const key = mergeFields.map(field => {
      if (!(field in fields) || fields[field] === null) throw new Error('merge field needs a non-null value');
      return JSON.stringify(fields[field]);
    }).join('\0');
    if (seen.has(key)) throw new Error('records repeat merge values');
    seen.add(key);
    return { fields };
  });
  return { performUpsert: { fieldsToMergeOn: mergeFields }, records, ...(input.typecast ? { typecast: true } : {}) };
}

function buildUpdateRequest(input: UpdateRecordsInput): Record<string, unknown> {
  validateBaseAndTable(input.baseId, input.tableIdOrName);
  if (!input.records || input.records.length < 1 || input.records.length > 10) throw new Error('records must hold 1 to 10 records');
  const seen = new Set<string>();
  const records = input.records.map((record, index) => {
    if (!recordIdPattern.test(record.id) || seen.has(record.id)) throw new Error('record ID is invalid or repeated');
    seen.add(record.id);
    validateWrittenFields(`records[${index}]`, record.fields);
    return { id: record.id, fields: record.fields };
  });
  return { records, ...(input.typecast ? { typecast: true } : {}) };
}

function decodeObject(body: Uint8Array): Record<string, unknown> {
  const document: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!isObject(document)) throw new Error('response is not an object');
  return document;
}

function isObject(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }
function requireString(value: unknown): string { if (typeof value !== 'string') throw new Error('expected string'); return value; }

function decodeRecord(value: unknown): AirtableRecord {
  if (!isObject(value) || typeof value.id !== 'string' || !recordIdPattern.test(value.id) || typeof value.createdTime !== 'string') throw new Error('record is invalid');
  const timestamp = Date.parse(value.createdTime);
  if (!Number.isFinite(timestamp)) throw new Error('createdTime is invalid');
  const fields = value.fields === undefined || value.fields === null ? {} : value.fields;
  if (!isObject(fields)) throw new Error('fields are invalid');
  const createdTime = new Date(timestamp).toISOString().replace(/\.000Z$/, 'Z');
  return { id: value.id, createdTime, fields };
}

function decodePartialSuccessReasons(value: unknown): string[] {
  if (!isObject(value) || value.message !== 'partialSuccess' || !Array.isArray(value.reasons)) return [];
  const allowed = new Set(['attachmentsFailedUploading', 'attachmentUploadRateIsTooHigh']);
  return [...new Set(value.reasons.filter((reason): reason is string => typeof reason === 'string' && allowed.has(reason)))];
}
