// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Buffer } from 'node:buffer';
import {
  newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry,
  type Call, type MutationAttempt, type MutationDefinition, type QueryAttempt,
  type QueryDefinition,
} from '@superdurable/connectors-sdk';
import { SalesforceClient, goQuery, isAPIName, isRecordID, salesforceFailure, type SalesforceOutcome } from './client.js';
import {
  GetRecordDefinition, QueryRecordsDefinition, UpdateRecordDefinition,
  UpsertRecordByExternalIDDefinition,
} from './definitions.js';
import type {
  GetRecordInput, QueryRecordsInput, QueryRecordsOutput, Record as SalesforceRecord,
  UpdateRecordInput, UpdateRecordOutput, UpsertRecordByExternalIDInput,
  UpsertRecordByExternalIDOutput,
} from './models.js';
import { renderSOQL } from './soql.js';

const locatorPattern = /^[A-Za-z0-9]{15,18}-[0-9]{1,10}$/;

function emptyRecord(): SalesforceRecord { return { type: '', fields: {} }; }
function emptyPage(): QueryRecordsOutput { return { records: [], totalSize: 0, isDone: false }; }

function isObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function parseExactJSON(body: Uint8Array): unknown {
  const text = Buffer.from(body).toString('utf8');
  if (Buffer.from(text).length !== body.length) throw new TypeError('invalid UTF-8 response');
  return JSON.parse(text, function (_key: string, value: unknown, ...additional: unknown[]) {
    if (typeof value !== 'number') return value;
    const context = additional[0] as { source?: string } | undefined;
    const rawJSON = JSON as unknown as { rawJSON?: (source: string) => unknown };
    if (!context?.source || !rawJSON.rawJSON) throw new TypeError(`response field ${_key || '<root>'} cannot be preserved exactly`);
    return rawJSON.rawJSON(context.source);
  }) as unknown;
}

function strictInteger(value: unknown, fieldName: string): number {
  const raw = typeof value === 'number' ? String(value) : JSON.stringify(value);
  if (!/^-?(?:0|[1-9][0-9]*)$/.test(raw ?? '')) throw new TypeError(`${fieldName} is not an integer`);
  const parsed = Number(raw);
  if (!Number.isSafeInteger(parsed)) throw new TypeError(`${fieldName} cannot be represented exactly`);
  return parsed;
}

function decodeRecord(value: unknown): SalesforceRecord {
  if (!isObject(value) || !isObject(value.attributes) || typeof value.attributes.type !== 'string')
    throw new TypeError('record attributes lack a valid sObject type');
  const type = value.attributes.type;
  if (!isAPIName(type) && type !== 'AggregateResult') throw new TypeError('record attributes lack a valid sObject type');
  const fields: Record<string, unknown> = {};
  for (const [key, item] of Object.entries(value)) if (key !== 'attributes') fields[key] = item;
  const id = fields.Id;
  if (id !== undefined && id !== null && (typeof id !== 'string' || !isRecordID(id))) throw new TypeError('record Id is invalid');
  return { type, ...(id ? { id } : {}), fields };
}

function validateRawFieldValue(value: unknown, fieldName: string): void {
  if (typeof value === 'number' && (!Number.isFinite(value) || Number.isInteger(value) && !Number.isSafeInteger(value)))
    throw new TypeError(`field ${fieldName} has an unrepresentable JSON number`);
  if (typeof value === 'bigint' || typeof value === 'function' || typeof value === 'symbol' || value === undefined)
    throw new TypeError(`field ${fieldName} must hold one JSON value`);
  if (Array.isArray(value)) value.forEach(item => validateRawFieldValue(item, fieldName));
  else if (isObject(value)) Object.values(value).forEach(item => validateRawFieldValue(item, fieldName));
}

function encodeFields(fields: Record<string, unknown | null> | null, excluded = ''): Uint8Array {
  const values = fields ?? {};
  if (Object.keys(values).length > 500) throw new TypeError('fields must hold at most 500 field values');
  const seen = new Set<string>();
  const ordered: Record<string, unknown> = {};
  for (const name of Object.keys(values).sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right)))) {
    if (!isAPIName(name)) throw new TypeError('fields keys must be field API names such as LastName');
    const lower = name.toLowerCase();
    if (lower === 'id' || lower === 'attributes' || seen.has(lower))
      throw new TypeError('fields must not set Id or attributes or repeat a field API name');
    if (excluded && lower === excluded.toLowerCase()) throw new TypeError(`fields must not set ${excluded}, which the request path already names`);
    seen.add(lower);
    validateRawFieldValue(values[name], name);
    ordered[name] = values[name];
  }
  const body = Buffer.from(JSON.stringify(ordered));
  if (body.length > 1 << 20) throw new TypeError('fields exceed the 1 MiB request limit');
  return body;
}

function queryOutcome<T>(client: SalesforceClient, call: Call, outcome: SalesforceOutcome, empty: T, routes: Record<string, string> = {}): QueryAttempt<T> {
  const branch = routes[outcome.route] ?? outcome.route;
  if (branch === 'retry') return newQueryRetry(outcome.failure, outcome.retryAfterMs);
  return newQueryBranch(branch, empty, client.receipt(call, '', outcome.apiUsage), outcome.failure);
}

function mutationOutcome<T>(client: SalesforceClient, call: Call, outcome: SalesforceOutcome, empty: T, routes: Record<string, string> = {}): MutationAttempt<T> {
  const branch = routes[outcome.route] ?? outcome.route;
  if (branch === 'retry') return newMutationRetry(outcome.failure, outcome.retryAfterMs);
  return newMutationBranch(branch, empty, client.receipt(call, '', outcome.apiUsage), outcome.failure);
}

/** One SOQL query page or cursor continuation. */
export class QueryRecordsOperation {
  /** Bind the shared Salesforce REST client. */
  constructor(private readonly client: SalesforceClient) {}
  /** Stable generated operation definition. */
  definition(): QueryDefinition { return QueryRecordsDefinition; }
  /** Validate input, execute one request, and decode the page. */
  async invoke(call: Call, input: QueryRecordsInput): Promise<QueryAttempt<QueryRecordsOutput>> {
    const empty = emptyPage();
    let batchSize: number;
    let path: string;
    let query: Record<string, string> | undefined;
    try {
      batchSize = input.batchSize || this.client.queryBatchSize;
      if (!Number.isSafeInteger(batchSize) || batchSize < 200 || batchSize > 2000)
        throw new TypeError('batchSize must be zero or from 200 to 2000');
      if (input.nextRecordsCursor) {
        if (input.soql?.trim() || input.bindings && Object.keys(input.bindings).length)
          throw new TypeError('set soql or nextRecordsCursor, not both');
        const prefix = this.client.dataPath('query') + '/';
        const locator = input.nextRecordsCursor.startsWith(prefix) ? input.nextRecordsCursor.slice(prefix.length) : '';
        if (!locatorPattern.test(locator)) throw new TypeError('nextRecordsCursor must be a cursor returned by queryRecords for this apiVersion');
        path = this.client.dataPath('query', locator);
      } else {
        const statement = renderSOQL(input.soql ?? '', input.bindings ?? {});
        query = { q: statement };
        if (Buffer.byteLength(goQuery(query)) > 15000) throw new TypeError('soql is too long for one Salesforce REST request');
        path = this.client.dataPath('query');
      }
    } catch (error) {
      return newQueryBranch('defect', empty, {}, salesforceFailure('queryRecords', 'VALIDATION', error instanceof Error ? error.message : 'invalid query'));
    }
    const credentials = await this.client.resolve(call, 'queryRecords');
    if ('route' in credentials) return queryOutcome(this.client, call, credentials, empty);
    const response = await this.client.exchange(call, credentials, 'queryRecords', { method: 'GET', path, ...(query ? { query } : {}), batchSize });
    if ('route' in response) return queryOutcome(this.client, call, response, empty, { notFound: 'providerRejected', requestRejected: 'queryRejected' });
    const receipt = this.client.receipt(call, '', response.apiUsage);
    let output: QueryRecordsOutput;
    try {
      const document = parseExactJSON(response.body);
      if (response.status !== 200 || !isObject(document) || typeof document.done !== 'boolean') throw new TypeError('query shape');
      const totalSize = strictInteger(document.totalSize, 'totalSize');
      if (totalSize < 0) throw new TypeError('totalSize is negative');
      const records = document.records == null ? [] : document.records;
      if (!Array.isArray(records)) throw new TypeError('records are invalid');
      let cursor = '';
      if (!document.done) {
        const prefix = this.client.dataPath('query') + '/';
        if (typeof document.nextRecordsUrl !== 'string' || !document.nextRecordsUrl.startsWith(prefix) || !locatorPattern.test(document.nextRecordsUrl.slice(prefix.length)))
          throw new TypeError('nextRecordsUrl is invalid');
        cursor = document.nextRecordsUrl;
      }
      output = { records: records.map(decodeRecord), totalSize, isDone: document.done, ...(cursor ? { nextRecordsCursor: cursor } : {}) };
    } catch {
      return newQueryBranch('invalidResponse', empty, receipt, salesforceFailure('queryRecords', 'PROTOCOL', 'Salesforce returned an invalid query page'));
    }
    if (output.totalSize === 0 && output.records?.length === 0 && output.isDone)
      return newQueryBranch('notFound', output, receipt, salesforceFailure('queryRecords', 'NOT_FOUND', 'no record matched the query'));
    return newQueryBranch('found', output, receipt);
  }
}

/** Read one Salesforce record by ID. */
export class GetRecordOperation {
  /** Bind the shared Salesforce REST client. */
  constructor(private readonly client: SalesforceClient) {}
  /** Stable generated operation definition. */
  definition(): QueryDefinition { return GetRecordDefinition; }
  /** Validate the field selection and decode one record. */
  async invoke(call: Call, input: GetRecordInput): Promise<QueryAttempt<SalesforceRecord>> {
    const empty = emptyRecord();
    const fields = input.fields ?? [];
    if (!isAPIName(input.sObjectType) || !isRecordID(input.recordId) || fields.length < 1 || fields.length > 200 || fields.some(field => !isAPIName(field)) || new Set(fields.map(field => field.toLowerCase())).size !== fields.length)
      return newQueryBranch('defect', empty, {}, salesforceFailure('getRecord', 'VALIDATION', 'record selection is invalid'));
    const credentials = await this.client.resolve(call, 'getRecord');
    if ('route' in credentials) return queryOutcome(this.client, call, credentials, empty);
    const response = await this.client.exchange(call, credentials, 'getRecord', {
      method: 'GET', path: this.client.dataPath('sobjects', input.sObjectType, input.recordId),
      query: { fields: fields.join(',') },
    });
    if ('route' in response) return queryOutcome(this.client, call, response, empty, { requestRejected: 'providerRejected' });
    const receipt = this.client.receipt(call, input.recordId, response.apiUsage);
    let record: SalesforceRecord;
    try {
      record = decodeRecord(parseExactJSON(response.body));
      if (response.status !== 200 || record.type.toLowerCase() !== input.sObjectType.toLowerCase() || record.id && record.id.slice(0, 15) !== input.recordId.slice(0, 15))
        throw new TypeError('record identity differs');
    } catch {
      return newQueryBranch('invalidResponse', empty, receipt, salesforceFailure('getRecord', 'PROTOCOL', 'Salesforce returned an invalid record'));
    }
    if (!record.id) record = { ...record, id: input.recordId };
    return newQueryBranch('found', record, this.client.receipt(call, record.id, response.apiUsage));
  }
}

/** Upsert one record by a unique external ID value. */
export class UpsertRecordByExternalIDOperation {
  /** Bind the shared Salesforce REST client. */
  constructor(private readonly client: SalesforceClient) {}
  /** Stable generated operation definition. */
  definition(): MutationDefinition { return UpsertRecordByExternalIDDefinition; }
  /** Derive a stable receipt key without sending it to Salesforce. */
  idempotencyKey(callID: string, input: UpsertRecordByExternalIDInput): string { void input; return callID; }
  /** Send a repeatable PATCH and classify every definite outcome. */
  async invoke(call: Call, input: UpsertRecordByExternalIDInput): Promise<MutationAttempt<UpsertRecordByExternalIDOutput>> {
    const empty: UpsertRecordByExternalIDOutput = {};
    const value = input.externalIdValue;
    if (!isAPIName(input.sObjectType) || !isAPIName(input.externalIdField) || input.externalIdField.toLowerCase() === 'id' || !value || value.trim() !== value || [...value].length > 255 || value === '.' || value === '..' || /\p{Cc}/u.test(value))
      return newMutationBranch('defect', empty, {}, salesforceFailure('upsertRecordByExternalId', 'VALIDATION', 'external ID selection is invalid'));
    let body: Uint8Array;
    try { body = encodeFields(input.fields, input.externalIdField); }
    catch (error) { return newMutationBranch('defect', empty, {}, salesforceFailure('upsertRecordByExternalId', 'VALIDATION', error instanceof Error ? error.message : 'invalid fields')); }
    const credentials = await this.client.resolve(call, 'upsertRecordByExternalId');
    if ('route' in credentials) return mutationOutcome(this.client, call, credentials, empty);
    const response = await this.client.exchange(call, credentials, 'upsertRecordByExternalId', {
      method: 'PATCH', path: this.client.dataPath('sobjects', input.sObjectType, input.externalIdField, value),
      body, duplicateRetry: true, oversizedRoute: 'retry',
    });
    if ('route' in response) return mutationOutcome(this.client, call, response, {
      ...(response.providerErrors.length ? { providerErrors: response.providerErrors } : {}),
    }, { notFound: 'providerRejected', requestRejected: 'recordRejected' });
    const receipt = this.client.receipt(call, '', response.apiUsage);
    if (response.status === 300) {
      let matchingRecordIds: string[] = [];
      try {
        const paths = JSON.parse(Buffer.from(response.body).toString('utf8')) as unknown;
        if (Array.isArray(paths)) matchingRecordIds = paths.filter((path): path is string => typeof path === 'string')
          .map(path => path.split('/').filter(Boolean).at(-1) ?? '').filter(isRecordID);
      } catch { /* A malformed 300 still conclusively means multiple matches. */ }
      return newMutationBranch('multipleMatches', matchingRecordIds.length ? { matchingRecordIds } : {}, receipt,
        salesforceFailure('upsertRecordByExternalId', 'CONFLICT', 'more than one record carries the external ID value'));
    }
    try {
      const document = JSON.parse(Buffer.from(response.body).toString('utf8')) as unknown;
      if (!isObject(document) || document.success !== true || typeof document.created !== 'boolean' || typeof document.id !== 'string' || !isRecordID(document.id))
        throw new TypeError('upsert result is invalid');
      const output: UpsertRecordByExternalIDOutput = { id: document.id, ...(document.created ? { isCreated: true } : {}) };
      return newMutationBranch('upserted', output, this.client.receipt(call, document.id, response.apiUsage));
    } catch {
      return newMutationRetry(salesforceFailure('upsertRecordByExternalId', 'PROTOCOL', 'Salesforce returned an unreadable upsert result'));
    }
  }
}

/** Update selected Salesforce record fields by ID. */
export class UpdateRecordOperation {
  /** Bind the shared Salesforce REST client. */
  constructor(private readonly client: SalesforceClient) {}
  /** Stable generated operation definition. */
  definition(): MutationDefinition { return UpdateRecordDefinition; }
  /** Derive a stable receipt key for a convergent field update. */
  idempotencyKey(callID: string, input: UpdateRecordInput): string { void input; return callID; }
  /** Send a repeatable PATCH and classify every definite outcome. */
  async invoke(call: Call, input: UpdateRecordInput): Promise<MutationAttempt<UpdateRecordOutput>> {
    const empty: UpdateRecordOutput = {};
    if (!isAPIName(input.sObjectType) || !isRecordID(input.recordId) || !input.fields || !Object.keys(input.fields).length)
      return newMutationBranch('defect', empty, {}, salesforceFailure('updateRecord', 'VALIDATION', 'record update selection is invalid'));
    let body: Uint8Array;
    try { body = encodeFields(input.fields); }
    catch (error) { return newMutationBranch('defect', empty, {}, salesforceFailure('updateRecord', 'VALIDATION', error instanceof Error ? error.message : 'invalid fields')); }
    const credentials = await this.client.resolve(call, 'updateRecord');
    if ('route' in credentials) return mutationOutcome(this.client, call, credentials, empty);
    const response = await this.client.exchange(call, credentials, 'updateRecord', {
      method: 'PATCH', path: this.client.dataPath('sobjects', input.sObjectType, input.recordId),
      body, oversizedRoute: 'retry',
    });
    if ('route' in response) return mutationOutcome(this.client, call, response, {
      ...(response.providerErrors.length ? { providerErrors: response.providerErrors } : {}),
    }, { requestRejected: 'recordRejected' });
    const receipt = this.client.receipt(call, input.recordId, response.apiUsage);
    if (response.status === 300) return newMutationBranch('providerRejected', empty, receipt,
      salesforceFailure('updateRecord', 'PROTOCOL', 'Salesforce returned multiple choices for a record ID'));
    return newMutationBranch('updated', { id: input.recordId }, receipt);
  }
}
