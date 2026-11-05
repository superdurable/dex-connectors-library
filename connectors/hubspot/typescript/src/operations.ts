// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { ExchangeFailure, HubSpotClient } from './client.js';
import { GetObjectDefinition, SearchObjectsDefinition, UpdateObjectDefinition, UpsertObjectDefinition } from './generated/definitions.js';
import { CRMObjectSchema, type CRMObject, type GetObjectInput, type ObjectPage, type SearchFilter, type SearchObjectsInput, type UpdateObjectInput, type UpsertedObject, type UpsertObjectInput } from './generated/models.js';

const propertyName = /^[A-Za-z0-9][A-Za-z0-9_]{0,99}$/;
const associationName = /^associations\.[a-z][a-z0-9_]{0,63}$/;
const recordId = /^[0-9]{1,20}$/;
const cursor = /^[0-9]{1,5}$/;
const zeroObject: CRMObject = { objectType: '', id: '', archived: false };
const zeroPage: ObjectPage = { objectType: '', total: 0, objects: [] };
const zeroUpsert: UpsertedObject = { object: zeroObject, created: false };

type ObjectType = SearchObjectsInput['objectType'];

/** Search one bounded CRM page. */
export class SearchObjectsOperation implements Query<SearchObjectsInput, ObjectPage> {
  /** Bind a client. */ constructor(private readonly client: HubSpotClient) {}
  /** Return the manifest definition. */ definition() { return SearchObjectsDefinition; }
  /** Validate, send, and decode a search. */
  async invoke(call: Call, input: SearchObjectsInput): Promise<QueryAttempt<ObjectPage>> {
    let body: Record<string, unknown>;
    try { body = searchBody(input); } catch (error) { return queryFailure('searchObjects', 'defect', 'VALIDATION', zeroPage, {}, error); }
    const result = await this.client.exchange(call, 'searchObjects', '', 'POST', objectsPath(input.objectType, 'search'), undefined, body);
    if ('outcome' in result) return queryOutcome(result.outcome, zeroPage, 'providerRejected');
    const receipt = this.client.receipt(call, result.response);
    try {
      const decoded = jsonObject(result.response.body);
      const total = integerField(decoded, 'total');
      if (total < 0) throw new TypeError('HubSpot returned a malformed response');
      const results = decoded.results;
      if (results !== undefined && results !== null && !Array.isArray(results)) throw new TypeError('HubSpot returned a malformed response');
      const objects = (results ?? [] as unknown[]).map(item => decodeCRMObject(input.objectType, item));
      const page: ObjectPage = { objectType: input.objectType, total, objects };
      if (decoded.paging !== undefined && decoded.paging !== null) {
        const paging = requireObject(decoded.paging);
        if (paging.next !== undefined && paging.next !== null) {
          const after = stringField(requireObject(paging.next), 'after');
          if (after) { if (!cursor.test(after)) throw new TypeError('HubSpot returned an invalid search cursor'); page.nextAfter = after; }
        }
      }
      return newQueryBranch('searched', page, receipt);
    } catch (error) { return queryFailure('searchObjects', 'invalidResponse', 'PROTOCOL', zeroPage, receipt, error); }
  }
}

/** Get one CRM record. */
export class GetObjectOperation implements Query<GetObjectInput, CRMObject> {
  /** Bind a client. */ constructor(private readonly client: HubSpotClient) {}
  /** Return the manifest definition. */ definition() { return GetObjectDefinition; }
  /** Validate, send, and decode one record. */
  async invoke(call: Call, input: GetObjectInput): Promise<QueryAttempt<CRMObject>> {
    let query: string;
    try { validateObjectType(input.objectType); validateRecordId(input.objectId); const properties = uniqueProperties(input.properties ?? []); query = `archived=false${properties.length ? `&properties=${goQueryEscape(properties.join(','))}` : ''}`; }
    catch (error) { return queryFailure('getObject', 'defect', 'VALIDATION', zeroObject, {}, error); }
    const result = await this.client.exchange(call, 'getObject', input.objectId, 'GET', objectsPath(input.objectType, input.objectId), query);
    if ('outcome' in result) return queryOutcome(result.outcome, zeroObject, 'providerRejected', 'notFound');
    const receipt = this.client.receipt(call, result.response, input.objectId);
    try { const object = decodeCRMObject(input.objectType, jsonObject(result.response.body)); if (object.id !== input.objectId) throw new TypeError('HubSpot returned an invalid record'); return newQueryBranch('found', object, receipt); }
    catch { return queryFailure('getObject', 'invalidResponse', 'PROTOCOL', zeroObject, receipt, new Error('HubSpot returned an invalid record')); }
  }
}

/** Upsert one CRM record by a unique property. */
export class UpsertObjectOperation implements Mutation<UpsertObjectInput, UpsertedObject> {
  /** Bind a client. */ constructor(private readonly client: HubSpotClient) {}
  /** Return the manifest definition. */ definition() { return UpsertObjectDefinition; }
  /** Reuse the stable Call ID for a repeat-safe upsert. */ idempotencyKey(callId: string) { return callId; }
  /** Validate and dispatch, repeating one conflicting upsert. */
  async invoke(call: Call, input: UpsertObjectInput): Promise<MutationAttempt<UpsertedObject>> {
    let body: Record<string, unknown>;
    try { validateObjectType(input.objectType); if (!propertyName.test(input.idProperty)) throw new Error('upsert ID property name is invalid'); if (!input.idValue.trim() || [...input.idValue].length > 65_536) throw new Error("upsert ID value must be non-empty and within HubSpot's property value limit"); validateValues(input.properties ?? {}, false); body = { inputs: [{ idProperty: input.idProperty, id: input.idValue, properties: input.properties ?? {} }] }; }
    catch (error) { return mutationFailure('upsertObject', 'defect', 'VALIDATION', zeroUpsert, {}, error); }
    for (let attempt = 0; attempt < 2; attempt++) {
      const result = await this.client.exchange(call, 'upsertObject', '', 'POST', objectsPath(input.objectType, 'batch/upsert'), undefined, body);
      if ('outcome' in result) { if (result.outcome.disposition === 'conflict' && attempt === 0) continue; return mutationOutcome(result.outcome, zeroUpsert, 'providerRejected', 'providerRejected', 'conflict'); }
      const receipt = this.client.receipt(call, result.response);
      try {
        const decoded = jsonObject(result.response.body);
        const results = arrayField(decoded, 'results');
        if (results.length === 0) {
          const errors = arrayField(decoded, 'errors');
          if (errors.length) {
            const first = requireObject(errors[0]);
            const statusText = stringField(first, 'status');
            let status = Number(statusText);
            if (!Number.isInteger(status) || status < 400) status = 400;
            const category = stringField(first, 'category');
            const syntheticBody = Buffer.from(JSON.stringify({ category }));
            const classified = this.client.classifyBatchError('upsertObject', { status, headers: result.response.headers, body: syntheticBody }, receipt);
            if (classified.disposition === 'conflict' && attempt === 0) continue;
            return mutationOutcome(classified, zeroUpsert, 'providerRejected', 'providerRejected', 'conflict');
          }
          throw new TypeError('HubSpot upsert response has no record');
        }
        if (results.length !== 1) throw new TypeError('HubSpot upsert response has more than one record');
        const providerObject = requireObject(results[0]);
        const object = decodeCRMObject(input.objectType, providerObject);
        const created = booleanField(providerObject, 'new');
        return newMutationBranch('upserted', { object, created }, { ...receipt, providerObjectId: object.id });
      } catch (error) { return mutationFailure('upsertObject', 'invalidResponse', 'PROTOCOL', zeroUpsert, receipt, error); }
    }
    throw new TypeError('unreachable upsert attempt');
  }
}

/** Replace named properties on one CRM record. */
export class UpdateObjectOperation implements Mutation<UpdateObjectInput, CRMObject> {
  /** Bind a client. */ constructor(private readonly client: HubSpotClient) {}
  /** Return the manifest definition. */ definition() { return UpdateObjectDefinition; }
  /** Reuse the stable Call ID for an absolute update. */ idempotencyKey(callId: string) { return callId; }
  /** Validate, send, and decode one update. */
  async invoke(call: Call, input: UpdateObjectInput): Promise<MutationAttempt<CRMObject>> {
    try { validateObjectType(input.objectType); validateRecordId(input.objectId); validateValues(input.properties ?? undefined, true); }
    catch (error) { return mutationFailure('updateObject', 'defect', 'VALIDATION', zeroObject, {}, error); }
    const result = await this.client.exchange(call, 'updateObject', input.objectId, 'PATCH', objectsPath(input.objectType, input.objectId), undefined, { properties: input.properties });
    if ('outcome' in result) return mutationOutcome(result.outcome, zeroObject, 'providerRejected', 'notFound', 'conflict');
    const receipt = this.client.receipt(call, result.response, input.objectId);
    try { const object = decodeCRMObject(input.objectType, jsonObject(result.response.body)); if (object.id !== input.objectId) throw new TypeError('HubSpot returned an invalid record'); return newMutationBranch('updated', object, receipt); }
    catch { return mutationFailure('updateObject', 'invalidResponse', 'PROTOCOL', zeroObject, receipt, new Error('HubSpot returned an invalid record')); }
  }
}

function queryOutcome<T>(outcome: ExchangeFailure, zero: T, rejected: string, notFound = rejected): QueryAttempt<T> {
  if (outcome.disposition === 'retry') return newQueryRetry(outcome.failure, outcome.retryAfterMs);
  const branch = outcome.disposition === 'notFound' ? notFound : outcome.disposition === 'conflict' || outcome.disposition === 'rejected' ? rejected : outcome.disposition;
  return newQueryBranch(branch, zero, outcome.receipt, outcome.failure);
}
function mutationOutcome<T>(outcome: ExchangeFailure, zero: T, rejected: string, notFound = rejected, conflict = rejected): MutationAttempt<T> {
  if (outcome.disposition === 'retry') return newMutationRetry(outcome.failure, outcome.retryAfterMs);
  const branch = outcome.disposition === 'notFound' ? notFound : outcome.disposition === 'conflict' ? conflict : outcome.disposition === 'rejected' ? rejected : outcome.disposition;
  return newMutationBranch(branch, zero, outcome.receipt, outcome.failure);
}
function queryFailure<T>(operation: string, branch: string, kind: Failure['kind'], zero: T, receipt: ReceiptDraft, error: unknown): QueryAttempt<T> {
  return newQueryBranch(branch, zero, receipt, { kind, provider: 'hubspot', operation, message: error instanceof Error ? error.message : 'HubSpot returned a malformed response' });
}
function mutationFailure<T>(operation: string, branch: string, kind: Failure['kind'], zero: T, receipt: ReceiptDraft, error: unknown): MutationAttempt<T> {
  return newMutationBranch(branch, zero, receipt, { kind, provider: 'hubspot', operation, message: error instanceof Error ? error.message : 'HubSpot returned a malformed response' });
}
function validateObjectType(value: ObjectType): void { if (!['contacts', 'companies', 'deals'].includes(value)) throw new Error('object type must be contacts, companies, or deals'); }
function validateRecordId(value: string): void { if (!recordId.test(value)) throw new Error('object ID must be a decimal HubSpot record ID'); }
function uniqueProperties(values: string[]): string[] { if (values.length > 100) throw new Error('at most 100 properties can be requested'); const unique = [...new Set(values)]; if (unique.some(value => !propertyName.test(value))) throw new Error('requested property name is invalid'); return unique; }
function validateValues(properties: Record<string, string> | undefined, required: boolean): void { if (required && !Object.keys(properties ?? {}).length) throw new Error('at least one property value is required'); if (Object.keys(properties ?? {}).length > 100) throw new Error('at most 100 property values can be written'); for (const [name, value] of Object.entries(properties ?? {})) { if (!propertyName.test(name)) throw new Error('written property name is invalid'); if ([...value].length > 65_536) throw new Error('property values cannot exceed 65536 characters'); } }
function objectsPath(objectType: ObjectType, suffix: string): string { return `crm/objects/2026-09/${objectType}/${suffix}`; }
function goQueryEscape(value: string): string { return encodeURIComponent(value).replace(/[!'()*~]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%20/g, '+'); }
function searchBody(input: SearchObjectsInput): Record<string, unknown> {
  validateObjectType(input.objectType);
  const groups = input.filterGroups ?? [];
  if (groups.length > 5) throw new Error('search allows at most 5 filter groups');
  let count = 0;
  for (const group of groups) { if (!group.filters || group.filters.length < 1 || group.filters.length > 6) throw new Error('each filter group needs 1 to 6 filters'); count += group.filters.length; group.filters.forEach(validateFilter); }
  if (count > 18) throw new Error('search allows at most 18 filters in total');
  const properties = uniqueProperties(input.properties ?? []);
  const limit = input.limit || 10;
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) throw new Error('search limit must be from 1 through 200');
  if (input.after && !cursor.test(input.after)) throw new Error('search after cursor must be the decimal NextAfter value of a previous page');
  if (Number(input.after || '0') + limit > 10_000) throw new Error("search paging cannot pass HubSpot's 10000-result limit");
  const body: Record<string, unknown> = { ...(groups.length ? { filterGroups: groups.map(group => ({ filters: group.filters })) } : {}), ...(input.query ? { query: input.query } : {}),
    ...(input.sort ? { sorts: [input.sort] } : {}), ...(properties.length ? { properties } : {}), limit, ...(input.after ? { after: input.after } : {}) };
  if (input.sort && (!propertyName.test(input.sort.propertyName) || !['ASCENDING', 'DESCENDING'].includes(input.sort.direction))) throw new Error('sort property or direction is invalid');
  if (Buffer.byteLength(JSON.stringify(body)) > 3_000) throw new Error("search request exceeds HubSpot's 3000-character body limit");
  return body;
}
function validateFilter(filter: SearchFilter): void {
  if (!propertyName.test(filter.propertyName) && !associationName.test(filter.propertyName)) throw new Error('filter property name is invalid');
  const hasValue = Boolean(filter.value), hasHigh = Boolean(filter.highValue), hasValues = Boolean(filter.values?.length);
  const operator = filter.operator;
  if (['EQ', 'NEQ', 'LT', 'LTE', 'GT', 'GTE', 'CONTAINS_TOKEN', 'NOT_CONTAINS_TOKEN'].includes(operator)) { if (!hasValue || hasHigh || hasValues) throw new Error(`filter operator ${operator} needs only a value`); }
  else if (operator === 'BETWEEN') { if (!hasValue || !hasHigh || hasValues) throw new Error('filter operator BETWEEN needs a value and a highValue'); }
  else if (operator === 'IN' || operator === 'NOT_IN') { if (hasValue || hasHigh || !hasValues || (filter.values?.length ?? 0) > 100 || filter.values?.some(value => !value)) throw new Error(`filter operator ${operator} needs 1 to 100 values`); }
  else if (operator === 'HAS_PROPERTY' || operator === 'NOT_HAS_PROPERTY') { if (hasValue || hasHigh || hasValues) throw new Error(`filter operator ${operator} takes no value`); }
  else throw new Error('filter operator is not supported');
}
function requireObject(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('HubSpot returned a malformed response'); return value as Record<string, unknown>; }
function jsonObject(body: Uint8Array): Record<string, unknown> { return requireObject(JSON.parse(Buffer.from(body).toString()) as unknown); }
function stringField(value: Record<string, unknown>, field: string): string { const candidate = value[field]; if (candidate === null || candidate === undefined) return ''; if (typeof candidate !== 'string') throw new TypeError('HubSpot returned a malformed response'); return candidate; }
function integerField(value: Record<string, unknown>, field: string): number { const candidate = value[field]; if (candidate === null || candidate === undefined) return 0; if (!Number.isSafeInteger(candidate)) throw new TypeError('HubSpot returned a malformed response'); return candidate as number; }
function booleanField(value: Record<string, unknown>, field: string): boolean { const candidate = value[field]; if (candidate === null || candidate === undefined) return false; if (typeof candidate !== 'boolean') throw new TypeError('HubSpot returned a malformed response'); return candidate; }
function arrayField(value: Record<string, unknown>, field: string): unknown[] { const candidate = value[field]; if (candidate === null || candidate === undefined) return []; if (!Array.isArray(candidate)) throw new TypeError('HubSpot returned a malformed response'); return candidate; }
function decodeCRMObject(objectType: ObjectType, value: unknown): CRMObject {
  const provider = requireObject(value);
  const id = stringField(provider, 'id');
  if (!recordId.test(id)) throw new TypeError('HubSpot returned a record without a valid ID');
  const properties = provider.properties === null || provider.properties === undefined ? undefined : requireObject(provider.properties);
  const cleaned: Record<string, string> = {};
  for (const [name, property] of Object.entries(properties ?? {})) { if (property === null) continue; if (typeof property !== 'string') throw new TypeError('HubSpot returned a malformed response'); cleaned[name] = property; }
  const createdAt = stringField(provider, 'createdAt');
  const updatedAt = stringField(provider, 'updatedAt');
  const result: CRMObject = { objectType, id, ...(Object.keys(cleaned).length ? { properties: cleaned } : {}),
    ...(createdAt ? { createdAt } : {}), ...(updatedAt ? { updatedAt } : {}), archived: booleanField(provider, 'archived') };
  const url = appURL(stringField(provider, 'url'));
  if (url) result.url = url;
  const parsed = CRMObjectSchema.parse(result);
  if (parsed.createdAt === '0001-01-01T00:00:00Z') delete parsed.createdAt;
  if (parsed.updatedAt === '0001-01-01T00:00:00Z') delete parsed.updatedAt;
  return parsed;
}
function appURL(value: string): string { try { const parsed = new URL(value); return parsed.protocol === 'https:' && !parsed.username && !parsed.password && parsed.hostname.endsWith('.hubspot.com') ? value : ''; } catch { return ''; } }
