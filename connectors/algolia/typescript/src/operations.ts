// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { Client } from './client.js';
import { SaveObjectDefinition, SearchDefinition } from './generated/definitions.js';
import { SaveObjectInputSchema, SearchInputSchema, type SavedObject, type SaveObjectInput, type SearchInput, type SearchPage } from './generated/models.js';

const emptyPage: SearchPage = { nbHits: 0, hits: null };
const emptySavedObject: SavedObject = { objectID: '', taskID: 0 };
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'algolia', operation, message }; }
function validPathPart(value: string): boolean { return value.trim() !== '' && !/[/?&#]/.test(value); }
function rejectionKind(status: number): Failure['kind'] { return status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION'; }
function encodePathPart(value: string): string { return encodeURIComponent(value).replace(/%24/g, '$').replace(/%26/g, '&').replace(/%2B/g, '+').replace(/%3A/g, ':').replace(/%3D/g, '=').replace(/%40/g, '@'); }

/** Query one Algolia index and return a bounded hit page. */
export class SearchOperation implements Query<SearchInput, SearchPage> {
  /** Bind the validated client. */
  constructor(private readonly client: Client) { if (!client) throw new TypeError('Algolia client is required'); }
  /** Return stable manifest metadata. */
  definition() { return SearchDefinition; }
  /** Validate and dispatch one search attempt. */
  async invoke(call: Call, input: SearchInput): Promise<QueryAttempt<SearchPage>> {
    if (!SearchInputSchema.safeParse(input).success || !validPathPart(input.indexName) || (input.hitsPerPage ?? 0) < 0 || (input.hitsPerPage ?? 0) > 1000) {
      return newQueryBranch('defect', emptyPage, zeroReceipt(), failure('search', 'VALIDATION', 'indexName or hitsPerPage is invalid'));
    }
    const params = new URLSearchParams({ query: input.query });
    if (input.hitsPerPage) params.set('hitsPerPage', String(input.hitsPerPage));
    params.sort();
    const response = await this.client.send(call, 'POST', `/indexes/${encodePathPart(input.indexName)}/query`, Buffer.from(JSON.stringify({ params: params.toString() })));
    if (response.kind === 'retry') return newQueryRetry(failure('search', response.failureKind, response.message), response.retryAfterMs);
    if (response.kind === 'failure') return newQueryBranch(response.failureKind === 'AUTHENTICATION' || response.failureKind === 'LOCAL_DEFECT' ? 'defect' : 'invalidResponse', emptyPage, zeroReceipt(), failure('search', response.failureKind, response.message));
    if (response.status !== 200) return newQueryBranch('providerRejected', emptyPage, zeroReceipt(), failure('search', rejectionKind(response.status), `Algolia rejected search with HTTP ${response.status}`));
    let page: unknown;
    try { page = JSON.parse(Buffer.from(response.body).toString('utf8')) as unknown; }
    catch { return newQueryBranch('invalidResponse', emptyPage, zeroReceipt(), failure('search', 'PROTOCOL', 'Algolia returned an invalid search page')); }
    if (!isSearchPage(page)) return newQueryBranch('invalidResponse', emptyPage, zeroReceipt(), failure('search', 'PROTOCOL', 'Algolia returned an invalid search page'));
    const value: SearchPage = { nbHits: page.nbHits ?? 0, hits: page.hits?.map(hit => ({ objectID: hit.objectID, ...(typeof hit.name === 'string' && hit.name !== '' ? { name: hit.name } : {}) })) ?? null };
    return newQueryBranch('searched', value, zeroReceipt());
  }
}

/** Replace a complete Algolia record by its stable object ID. */
export class SaveObjectOperation implements Mutation<SaveObjectInput, SavedObject> {
  /** Bind the validated client. */
  constructor(private readonly client: Client) { if (!client) throw new TypeError('Algolia client is required'); }
  /** Return stable manifest metadata. */
  definition() { return SaveObjectDefinition; }
  /** Use the Dex call identity while absolute replacement makes replay safe. */
  idempotencyKey(callId: string): string { return callId; }
  /** Validate and dispatch one replacement attempt. */
  async invoke(call: Call, input: SaveObjectInput): Promise<MutationAttempt<SavedObject>> {
    if (!SaveObjectInputSchema.safeParse(input).success || !validPathPart(input.indexName) || !validPathPart(input.objectID) || input.attributes === null) {
      return newMutationBranch('defect', emptySavedObject, zeroReceipt(), failure('saveObject', 'VALIDATION', 'indexName, objectID, and attributes are required'));
    }
    if (Object.hasOwn(input.attributes, 'objectID')) return newMutationBranch('defect', emptySavedObject, zeroReceipt(), failure('saveObject', 'VALIDATION', 'objectID belongs in its dedicated field'));
    if (Object.keys(input.attributes).some(key => !key.trim())) return newMutationBranch('defect', emptySavedObject, zeroReceipt(), failure('saveObject', 'VALIDATION', 'attribute names must be nonblank'));
    const body = Buffer.from(JSON.stringify({ ...input.attributes, objectID: input.objectID }));
    if (body.length > 1 << 20) return newMutationBranch('defect', emptySavedObject, zeroReceipt(), failure('saveObject', 'VALIDATION', 'record payload is invalid or too large'));
    const response = await this.client.send(call, 'PUT', `/indexes/${encodePathPart(input.indexName)}/${encodePathPart(input.objectID)}`, body);
    if (response.kind === 'retry') return newMutationRetry(failure('saveObject', response.failureKind, response.message), response.retryAfterMs);
    if (response.kind === 'failure') return newMutationBranch(response.failureKind === 'AUTHENTICATION' || response.failureKind === 'LOCAL_DEFECT' ? 'defect' : 'invalidResponse', emptySavedObject, zeroReceipt(), failure('saveObject', response.failureKind, response.message));
    if (response.status !== 200) return newMutationBranch('providerRejected', emptySavedObject, zeroReceipt(), failure('saveObject', rejectionKind(response.status), `Algolia rejected record replacement with HTTP ${response.status}`));
    let result: unknown;
    try { result = JSON.parse(Buffer.from(response.body).toString('utf8')) as unknown; }
    catch { return newMutationBranch('invalidResponse', emptySavedObject, zeroReceipt(), failure('saveObject', 'PROTOCOL', 'Algolia returned an invalid indexing task')); }
    if (!isSavedObject(result, input.objectID)) return newMutationBranch('invalidResponse', emptySavedObject, zeroReceipt(), failure('saveObject', 'PROTOCOL', 'Algolia returned an invalid indexing task'));
    return newMutationBranch('saved', { objectID: result.objectID, taskID: result.taskID }, zeroReceipt());
  }
}

function isSearchPage(value: unknown): value is SearchPage {
  if (!value || typeof value !== 'object') return false;
  const page = value as Record<string, unknown>;
  const count = page.nbHits;
  return (count == null || (typeof count === 'number' && Number.isSafeInteger(count) && count >= 0)) && Array.isArray(page.hits) && page.hits.every(hit => hit && typeof hit === 'object' && typeof hit.objectID === 'string' && hit.objectID !== '' && (hit.name === undefined || hit.name === null || typeof hit.name === 'string'));
}
function isSavedObject(value: unknown, objectID: string): value is SavedObject {
  if (!value || typeof value !== 'object') return false;
  const record = value as Record<string, unknown>;
  return record.objectID === objectID && Number.isSafeInteger(record.taskID) && (record.taskID as number) > 0;
}
