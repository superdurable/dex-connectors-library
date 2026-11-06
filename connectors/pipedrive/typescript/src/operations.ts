// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { newFence, CheckpointError, InsufficientTimeError } from '@superdurable/connectors-sdk/singledispatch';
import type { PipedriveClient, PipedriveExchange, PipedriveSession } from './client.js';
import { CreateObjectDefinition, GetObjectDefinition, ListObjectsDefinition, SearchObjectsDefinition, UpdateObjectDefinition, UpsertObjectDefinition } from './generated/definitions.js';
import type { CRMObject, CreateObjectInput, GetObjectInput, ListObjectsInput, ObjectPage, SearchObjectsInput, UpdateObjectInput, UpsertedObject, UpsertObjectInput } from './generated/models.js';

const idPattern = /^[1-9][0-9]{0,18}$/;
const cursorPattern = /^[A-Za-z0-9_\-=+/.]{1,1000}$/;
const standardFieldPattern = /^[a-z][a-z0-9_]{0,63}$/;
const customKeyPattern = /^[0-9a-f]{40}$/;
const validObjectTypes = ['persons', 'organizations', 'deals'];
const zeroObject: CRMObject = { objectType: '', id: '' };
const zeroPage: ObjectPage = { objectType: '', objects: [] };
const zeroUpsert: UpsertedObject = { object: zeroObject, created: false };

/** Search one bounded page of partial Pipedrive records. */
export class SearchObjectsOperation implements Query<SearchObjectsInput, ObjectPage> {
  /** Bind a provider client. */ constructor(private readonly client: PipedriveClient) {}
  /** Return the manifest definition. */ definition() { return SearchObjectsDefinition; }
  /** Validate, request, and classify one page. */
  async invoke(call: Call, input: SearchObjectsInput): Promise<QueryAttempt<ObjectPage>> {
    let query: URLSearchParams;
    try { query = searchQuery(input); } catch (error) { return defectQuery(this.client, 'searchObjects', zeroPage, error); }
    const started = await this.client.startSession(call, 'searchObjects');
    if (started.kind !== 'resolved') return sessionQuery(started, 'providerRejected', zeroPage);
    const result = await this.client.exchange(call, 'searchObjects', started.session, 'GET', `/api/v2/${input.objectType}/search`, query);
    const failed = failedQuery(this.client, call, result, zeroPage, false);
    if (failed) return failed;
    try { return newQueryBranch('searched', decodePage(input.objectType, result.response!, true), this.client.receipt(call, result.response)); }
    catch { return invalidQuery(this.client, call, 'searchObjects', zeroPage, result); }
  }
}

/** Read one full Pipedrive record by ID. */
export class GetObjectOperation implements Query<GetObjectInput, CRMObject> {
  /** Bind a provider client. */ constructor(private readonly client: PipedriveClient) {}
  /** Return the manifest definition. */ definition() { return GetObjectDefinition; }
  /** Validate, request, and classify one record. */
  async invoke(call: Call, input: GetObjectInput): Promise<QueryAttempt<CRMObject>> {
    try { requireObjectType(input.objectType); requireId(input.objectId, 'objectId'); }
    catch (error) { return defectQuery(this.client, 'getObject', zeroObject, error); }
    const started = await this.client.startSession(call, 'getObject');
    if (started.kind !== 'resolved') return sessionQuery(started, 'providerRejected', zeroObject);
    const result = await this.client.exchange(call, 'getObject', started.session, 'GET', `/api/v2/${input.objectType}/${input.objectId}`);
    const failed = failedQuery(this.client, call, result, zeroObject, true, input.objectId);
    if (failed) return failed;
    try { return newQueryBranch('found', decodeMatchingRecord(input.objectType, input.objectId, result), this.client.receipt(call, result.response, input.objectId)); }
    catch { return invalidQuery(this.client, call, 'getObject', zeroObject, result, input.objectId); }
  }
}

/** List one bounded page of full Pipedrive records. */
export class ListObjectsOperation implements Query<ListObjectsInput, ObjectPage> {
  /** Bind a provider client. */ constructor(private readonly client: PipedriveClient) {}
  /** Return the manifest definition. */ definition() { return ListObjectsDefinition; }
  /** Validate, request, and classify one page. */
  async invoke(call: Call, input: ListObjectsInput): Promise<QueryAttempt<ObjectPage>> {
    let query: URLSearchParams;
    try { query = listQuery(input); } catch (error) { return defectQuery(this.client, 'listObjects', zeroPage, error); }
    const started = await this.client.startSession(call, 'listObjects');
    if (started.kind !== 'resolved') return sessionQuery(started, 'providerRejected', zeroPage);
    const result = await this.client.exchange(call, 'listObjects', started.session, 'GET', `/api/v2/${input.objectType}`, query);
    const failed = failedQuery(this.client, call, result, zeroPage, false);
    if (failed) return failed;
    try { return newQueryBranch('listed', decodePage(input.objectType, result.response!, false), this.client.receipt(call, result.response)); }
    catch { return invalidQuery(this.client, call, 'listObjects', zeroPage, result); }
  }
}

/** Apply one repeatable field update to an existing record. */
export class UpdateObjectOperation implements Mutation<UpdateObjectInput, CRMObject> {
  /** Bind a provider client. */ constructor(private readonly client: PipedriveClient) {}
  /** Return the manifest definition. */ definition() { return UpdateObjectDefinition; }
  /** Correlate a receipt to one Call. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate, patch, and classify the returned record. */
  async invoke(call: Call, input: UpdateObjectInput): Promise<MutationAttempt<CRMObject>> {
    let body: Record<string, unknown>;
    try { requireObjectType(input.objectType); requireId(input.objectId, 'objectId'); body = writeBody(input.fields, input.customFields, false); if (!Object.keys(body).length) throw Error('at least one field or custom field value is required'); }
    catch (error) { return defectMutation(this.client, 'updateObject', zeroObject, error); }
    const started = await this.client.startSession(call, 'updateObject');
    if (started.kind !== 'resolved') return sessionMutation(started, 'providerRejected', zeroObject);
    const result = await this.client.exchange(call, 'updateObject', started.session, 'PATCH', `/api/v2/${input.objectType}/${input.objectId}`, undefined, body);
    const failed = failedMutation(this.client, call, result, zeroObject, true, input.objectId, true);
    if (failed) return failed;
    try { return newMutationBranch('updated', decodeMatchingRecord(input.objectType, input.objectId, result), this.client.receipt(call, result.response, input.objectId)); }
    catch { return invalidMutation(this.client, call, 'updateObject', zeroObject, result, input.objectId); }
  }
}

/** Create one record behind a durable single-dispatch fence. */
export class CreateObjectOperation implements Mutation<CreateObjectInput, CRMObject> {
  /** Bind a provider client. */ constructor(private readonly client: PipedriveClient) {}
  /** Return the manifest definition. */ definition() { return CreateObjectDefinition; }
  /** Correlate the fence to one Call. */ idempotencyKey(callId: string): string { return callId; }
  /** Fence, send, and read back a prior accepted create when needed. */
  async invoke(call: Call, input: CreateObjectInput): Promise<MutationAttempt<CRMObject>> {
    let fence: ReturnType<typeof newFence>;
    try { fence = newFence(call, 30_000); } catch { return newMutationBranch('defect', zeroObject, {}, this.client.failure('createObject', 'LOCAL_DEFECT', 'single-dispatch fence could not be built')); }
    const decision = fence.decide();
    if (decision.kind !== 'sendAllowed') {
      const id = decision.kind === 'readBackRequired' ? decision.marker?.providerId : undefined;
      if (!id || decision.marker?.callId !== call.id) return newMutationUncertain(zeroObject, this.client.failure('createObject', 'TRANSPORT', 'an earlier create may have reached Pipedrive and cannot be confirmed'), this.client.receipt(call));
      const started = await this.client.startSession(call, 'createObject');
      if (started.kind !== 'resolved') return started.kind === 'retry' ? newMutationRetry(started.failure) : newMutationUncertain(zeroObject, started.failure, this.client.receipt(call, undefined, id));
      const read = await this.client.exchange(call, 'createObject', started.session, 'GET', `/api/v2/${input.objectType}/${id}`);
      if (isRetryable(read)) return newMutationRetry(read.failure!, read.retryAfterMs);
      if (read.kind !== 'success') return newMutationUncertain(zeroObject, read.failure!, this.client.receipt(call, read.response, id));
      try { return newMutationBranch('created', decodeMatchingRecord(input.objectType, id, read), this.client.receipt(call, read.response, id)); }
      catch { return newMutationUncertain(zeroObject, this.client.failure('createObject', 'PROTOCOL', 'Pipedrive returned an invalid record'), this.client.receipt(call, read.response, id)); }
    }
    let body: Record<string, unknown>;
    try { requireObjectType(input.objectType); body = writeBody(input.fields, input.customFields, true, input.objectType); }
    catch (error) { return defectMutation(this.client, 'createObject', zeroObject, error); }
    const started = await this.client.startSession(call, 'createObject');
    if (started.kind !== 'resolved') {
      try { await fence.recordNotSent(); } catch { return fenceRetry(this.client, 'createObject'); }
      return sessionMutation(started, 'providerRejected', zeroObject);
    }
    let result: PipedriveExchange;
    try { result = await fence.dispatch(signal => this.client.exchange(call, 'createObject', started.session, 'POST', `/api/v2/${input.objectType}`, undefined, body, signal, true)); }
    catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError) return fenceRetry(this.client, 'createObject');
      return newMutationUncertain(zeroObject, this.client.failure('createObject', 'TRANSPORT', 'create dispatch could not be confirmed'), {});
    }
    const receipt = this.client.receipt(call, result.response);
    if (result.response?.status === 401 && started.session.credentials.authMethodId === 'pipedrive-oauth' && await this.client.refreshAfterRejection(call, started.session)) {
      try { await fence.recordCanResend(); } catch { return fenceRetry(this.client, 'createObject'); }
      return newMutationRetry(result.failure!);
    }
    if (result.kind === 'notSent' || result.kind === 'rateLimited' || result.kind === 'unavailable' && result.response?.status === 503) {
      try { await fence.recordCanResend(); } catch { return fenceRetry(this.client, 'createObject'); }
      return newMutationRetry(result.failure!, result.retryAfterMs);
    }
    if (result.kind === 'unavailable' || result.kind === 'invalid') return newMutationUncertain(zeroObject, result.failure!, receipt);
    if (result.kind !== 'success') return newMutationBranch(result.kind === 'defect' ? 'defect' : 'providerRejected', zeroObject, receipt, result.failure);
    let object: CRMObject;
    try { object = decodeRecordBody(input.objectType, result.response!); }
    catch { return newMutationUncertain(zeroObject, this.client.failure('createObject', 'PROTOCOL', 'Pipedrive accepted the create but returned an invalid record'), receipt); }
    try { await fence.recordProviderId(object.id); } catch { /* Accepted result remains known when the extra identity checkpoint fails. */ }
    return newMutationBranch('created', object, this.client.receipt(call, result.response, object.id));
  }
}

/** Upsert a person by email or an organization by name. */
export class UpsertObjectOperation implements Mutation<UpsertObjectInput, UpsertedObject> {
  /** Bind a provider client. */ constructor(private readonly client: PipedriveClient) {}
  /** Return the manifest definition. */ definition() { return UpsertObjectDefinition; }
  /** Correlate the fence to one Call. */ idempotencyKey(callId: string): string { return callId; }
  /** Search, update, or fence a create. */
  async invoke(call: Call, input: UpsertObjectInput): Promise<MutationAttempt<UpsertedObject>> {
    let fence: ReturnType<typeof newFence>;
    try { fence = newFence(call, 30_000); } catch { return newMutationBranch('defect', zeroUpsert, {}, this.client.failure('upsertObject', 'LOCAL_DEFECT', 'single-dispatch fence could not be built')); }
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return uncertainUpsert(this.client, call, this.client.failure('upsertObject', 'TRANSPORT', 'create checkpoint could not be read'));
    let createBody: Record<string, unknown>; let updateBody: Record<string, unknown>; let identityField: string;
    try { ({ createBody, updateBody, identityField } = upsertBodies(input)); }
    catch (error) { return defectMutation(this.client, 'upsertObject', zeroUpsert, error); }
    const earlierCreate = decision.kind === 'readBackRequired';
    const started = await this.client.startSession(call, 'upsertObject');
    if (started.kind !== 'resolved') {
      if (!earlierCreate) { try { await fence.recordNotSent(); } catch { return fenceRetry(this.client, 'upsertObject'); } return sessionMutation(started, 'providerRejected', zeroUpsert); }
      return started.kind === 'retry' ? newMutationRetry(started.failure) : uncertainUpsert(this.client, call, started.failure, decision.marker?.providerId);
    }
    const session = started.session;
    if (earlierCreate && decision.marker?.providerId && decision.marker.callId === call.id) {
      const read = await this.client.exchange(call, 'upsertObject', session, 'GET', `/api/v2/${input.objectType}/${decision.marker.providerId}`);
      if (isRetryable(read)) return newMutationRetry(read.failure!, read.retryAfterMs);
      if (read.kind !== 'success') return uncertainUpsert(this.client, call, read.failure!, decision.marker.providerId, read);
      try { decodeMatchingRecord(input.objectType, decision.marker.providerId, read); }
      catch { return uncertainUpsert(this.client, call, this.client.failure('upsertObject', 'PROTOCOL', 'Pipedrive returned an invalid record'), decision.marker.providerId, read); }
      if (!Object.keys(updateBody).length) return newMutationBranch('upserted', { object: decodeMatchingRecord(input.objectType, decision.marker.providerId, read), created: false }, this.client.receipt(call, read.response, decision.marker.providerId));
      return this.updateMatch(call, session, input.objectType, decision.marker.providerId, updateBody, earlierCreate, fence);
    }
    const matches = await this.findMatches(call, session, input, identityField);
    if (matches.result.kind !== 'success') {
      if (earlierCreate && !isRetryable(matches.result)) return uncertainUpsert(this.client, call, matches.result.failure!, '', matches.result);
      if (!earlierCreate && isRetryable(matches.result)) { try { await fence.recordNotSent(); } catch { return fenceRetry(this.client, 'upsertObject'); } }
      return failedMutation(this.client, call, matches.result, zeroUpsert, true) ?? newMutationRetry(matches.result.failure!);
    }
    if (matches.ids.length > 1 || matches.hasMore) return newMutationBranch('multipleMatches', { ...zeroUpsert, matchingIds: matches.ids }, this.client.receipt(call, matches.result.response), this.client.failure('upsertObject', 'CONFLICT', `${Math.max(matches.ids.length, 2)} or more Pipedrive ${input.objectType} carry the identity value, so nothing was written`));
    if (matches.ids.length === 1) return this.updateMatch(call, session, input.objectType, matches.ids[0]!, updateBody, earlierCreate, fence);
    if (earlierCreate) return uncertainUpsert(this.client, call, this.client.failure('upsertObject', 'TRANSPORT', 'Pipedrive search finds no record with the identity value'));
    let result: PipedriveExchange;
    try { result = await fence.dispatch(signal => this.client.exchange(call, 'upsertObject', session, 'POST', `/api/v2/${input.objectType}`, undefined, createBody, signal, true)); }
    catch (error) { return error instanceof CheckpointError || error instanceof InsufficientTimeError ? fenceRetry(this.client, 'upsertObject') : uncertainUpsert(this.client, call, this.client.failure('upsertObject', 'TRANSPORT', 'create dispatch could not be confirmed')); }
    if (result.response?.status === 401 && session.credentials.authMethodId === 'pipedrive-oauth' && await this.client.refreshAfterRejection(call, session)) {
      try { await fence.recordCanResend(); } catch { return fenceRetry(this.client, 'upsertObject'); }
      return newMutationRetry(result.failure!);
    }
    if (result.kind === 'notSent' || result.kind === 'rateLimited' || result.kind === 'unavailable' && result.response?.status === 503) {
      try { await fence.recordCanResend(); } catch { return fenceRetry(this.client, 'upsertObject'); }
      return newMutationRetry(result.failure!, result.retryAfterMs);
    }
    if (result.kind === 'unavailable') return newMutationRetry(result.failure!, result.retryAfterMs);
    if (result.kind === 'invalid') return this.readBackAcceptedCreate(call, session, input, identityField, fence, result);
    if (result.kind !== 'success') return newMutationBranch(result.kind === 'defect' ? 'defect' : 'providerRejected', zeroUpsert, this.client.receipt(call, result.response), result.failure);
    try {
      const object = decodeRecordBody(input.objectType, result.response!);
      try { await fence.recordProviderId(object.id); } catch { /* Accepted result remains known without the extra identity checkpoint. */ }
      return newMutationBranch('upserted', { object, created: true }, this.client.receipt(call, result.response, object.id));
    } catch { return this.readBackAcceptedCreate(call, session, input, identityField, fence, result); }
  }

  private async findMatches(call: Call, session: PipedriveSession, input: UpsertObjectInput, identityField: string) {
    const query = new URLSearchParams({ term: input.idValue, fields: identityField, exact_match: 'true', limit: '50' });
    const result = await this.client.exchange(call, 'upsertObject', session, 'GET', `/api/v2/${input.objectType}/search`, query);
    if (result.kind !== 'success') return { ids: [] as string[], hasMore: false, result };
    try {
      const page = decodePage(input.objectType, result.response!, true);
      const ids = page.objects!.filter(object => input.objectType === 'organizations' ? object.name?.trim().toLowerCase() === input.idValue.toLowerCase() : object.emails?.some(email => email.trim().toLowerCase() === input.idValue.toLowerCase())).map(object => object.id);
      return { ids, hasMore: Boolean(page.nextCursor), result };
    } catch { return { ids: [] as string[], hasMore: false, result: { ...result, kind: 'invalid' as const, failure: this.client.failure('upsertObject', 'PROTOCOL', 'Pipedrive returned a malformed response') } }; }
  }

  private async updateMatch(call: Call, session: PipedriveSession, objectType: string, objectId: string, body: Record<string, unknown>, earlierCreate: boolean, fence: ReturnType<typeof newFence>): Promise<MutationAttempt<UpsertedObject>> {
    const result = await this.client.exchange(call, 'upsertObject', session, Object.keys(body).length ? 'PATCH' : 'GET', `/api/v2/${objectType}/${objectId}`, undefined, Object.keys(body).length ? body : undefined);
    if (result.kind !== 'success') {
      if (earlierCreate && !isRetryable(result)) return uncertainUpsert(this.client, call, result.failure!, objectId, result);
      if (!earlierCreate && isRetryable(result)) { try { await fence.recordNotSent(); } catch { return fenceRetry(this.client, 'upsertObject'); } }
      return failedMutation(this.client, call, result, zeroUpsert, true, objectId) ?? newMutationRetry(result.failure!);
    }
    try { return newMutationBranch('upserted', { object: decodeMatchingRecord(objectType, objectId, result), created: false }, this.client.receipt(call, result.response, objectId)); }
    catch { return earlierCreate ? uncertainUpsert(this.client, call, this.client.failure('upsertObject', 'PROTOCOL', 'Pipedrive returned an invalid record'), objectId, result) : invalidMutation(this.client, call, 'upsertObject', zeroUpsert, result, objectId); }
  }

  private async readBackAcceptedCreate(call: Call, session: PipedriveSession, input: UpsertObjectInput, identityField: string, fence: ReturnType<typeof newFence>, prior: PipedriveExchange): Promise<MutationAttempt<UpsertedObject>> {
    const matches = await this.findMatches(call, session, input, identityField);
    if (isRetryable(matches.result)) return newMutationRetry(matches.result.failure!, matches.result.retryAfterMs);
    if (matches.result.kind !== 'success' || matches.ids.length !== 1 || matches.hasMore) return uncertainUpsert(this.client, call, this.client.failure('upsertObject', 'PROTOCOL', 'Pipedrive accepted the create but identity read-back could not confirm one record'), '', prior);
    const id = matches.ids[0]!;
    const read = await this.client.exchange(call, 'upsertObject', session, 'GET', `/api/v2/${input.objectType}/${id}`);
    if (isRetryable(read)) return newMutationRetry(read.failure!, read.retryAfterMs);
    if (read.kind !== 'success') return uncertainUpsert(this.client, call, read.failure!, '', prior);
    try {
      const object = decodeMatchingRecord(input.objectType, id, read);
      try { await fence.recordProviderId(id); } catch { /* Accepted result remains known without the extra identity checkpoint. */ }
      return newMutationBranch('upserted', { object, created: true }, this.client.receipt(call, prior.response, id));
    } catch { return uncertainUpsert(this.client, call, this.client.failure('upsertObject', 'PROTOCOL', 'Pipedrive returned an invalid record'), '', prior); }
  }
}

function isRetryable(result: PipedriveExchange): boolean { return result.kind === 'notSent' || result.kind === 'rateLimited' || result.kind === 'unavailable'; }
function defectQuery<Output>(client: PipedriveClient, operation: string, zero: Output, error: unknown): QueryAttempt<Output> { return newQueryBranch('defect', zero, {}, client.failure(operation, 'VALIDATION', error instanceof Error ? error.message : 'invalid input')); }
function defectMutation<Output>(client: PipedriveClient, operation: string, zero: Output, error: unknown): MutationAttempt<Output> { return newMutationBranch('defect', zero, {}, client.failure(operation, 'VALIDATION', error instanceof Error ? error.message : 'invalid input')); }
function sessionQuery<Output>(result: Exclude<Awaited<ReturnType<PipedriveClient['startSession']>>, { kind: 'resolved' }>, rejected: string, zero: Output): QueryAttempt<Output> { return result.kind === 'retry' ? newQueryRetry(result.failure) : newQueryBranch(result.kind === 'defect' ? 'defect' : rejected, zero, {}, result.failure); }
function sessionMutation<Output>(result: Exclude<Awaited<ReturnType<PipedriveClient['startSession']>>, { kind: 'resolved' }>, rejected: string, zero: Output): MutationAttempt<Output> { return result.kind === 'retry' ? newMutationRetry(result.failure) : newMutationBranch(result.kind === 'defect' ? 'defect' : rejected, zero, {}, result.failure); }
function failedQuery<Output>(client: PipedriveClient, call: Call, result: PipedriveExchange, zero: Output, hasNotFound: boolean, objectId = ''): QueryAttempt<Output> | undefined { if (result.kind === 'success') return undefined; if (isRetryable(result)) return newQueryRetry(result.failure!, result.retryAfterMs); return newQueryBranch(result.kind === 'notFound' && hasNotFound ? 'notFound' : result.kind === 'invalid' ? 'invalidResponse' : result.kind === 'defect' ? 'defect' : 'providerRejected', zero, client.receipt(call, result.response, objectId), result.failure); }
function failedMutation<Output>(client: PipedriveClient, call: Call, result: PipedriveExchange, zero: Output, hasInvalid: boolean, objectId = '', hasNotFound = false): MutationAttempt<Output> | undefined { if (result.kind === 'success') return undefined; if (isRetryable(result)) return newMutationRetry(result.failure!, result.retryAfterMs); return newMutationBranch(result.kind === 'notFound' && hasNotFound ? 'notFound' : result.kind === 'invalid' && hasInvalid ? 'invalidResponse' : result.kind === 'defect' ? 'defect' : 'providerRejected', zero, client.receipt(call, result.response, objectId), result.failure); }
function invalidQuery<Output>(client: PipedriveClient, call: Call, operation: string, zero: Output, result: PipedriveExchange, id = ''): QueryAttempt<Output> { return newQueryBranch('invalidResponse', zero, client.receipt(call, result.response, id), client.failure(operation, 'PROTOCOL', 'Pipedrive returned a malformed response')); }
function invalidMutation<Output>(client: PipedriveClient, call: Call, operation: string, zero: Output, result: PipedriveExchange, id = ''): MutationAttempt<Output> { return newMutationBranch('invalidResponse', zero, client.receipt(call, result.response, id), client.failure(operation, 'PROTOCOL', 'Pipedrive returned an invalid record')); }
function fenceRetry<Output>(client: PipedriveClient, operation: string): MutationAttempt<Output> { return newMutationRetry(client.failure(operation, 'AVAILABILITY', 'The single-dispatch fence could not authorize a send or retry; the next attempt must reconcile the checkpoint')); }
function uncertainUpsert(client: PipedriveClient, call: Call, failure: Failure, id = '', result?: PipedriveExchange): MutationAttempt<UpsertedObject> { return newMutationUncertain(zeroUpsert, { ...failure, message: `this Step sent a create that was never confirmed, and ${failure.message}` }, client.receipt(call, result?.response, id)); }

function requireObjectType(type: string): void { if (!validObjectTypes.includes(type)) throw Error('object type must be persons, organizations, or deals'); }
function requireId(id: string, name: string): void { if (!idPattern.test(id)) throw Error(`${name} must be a positive decimal Pipedrive ID`); }
function requireCursor(cursor: string | undefined): void { if (cursor && !cursorPattern.test(cursor)) throw Error('cursor must be the nextCursor of a previous page'); }
function pageLimit(limit: number | undefined): number { if (!limit) return 25; if (!Number.isInteger(limit) || limit < 1 || limit > 500) throw Error('limit must be from 1 through 500'); return limit; }
function joinStatuses(statuses: string[] | undefined, allowsDeleted: boolean): string { const names: string[] = []; for (const status of statuses ?? []) { if (!['open', 'won', 'lost', ...(allowsDeleted ? ['deleted'] : [])].includes(status)) throw Error(`deal status ${JSON.stringify(status)} is not supported here`); if (!names.includes(status)) names.push(status); } return names.join(','); }

function searchQuery(input: SearchObjectsInput): URLSearchParams {
  requireObjectType(input.objectType);
  const length = [...input.term.trim()].length;
  const minimum = input.exactMatch ? 1 : 2;
  if (length < minimum || [...input.term].length > 255) throw Error(`search term needs ${minimum} to 255 characters`);
  const query = new URLSearchParams({ term: input.term });
  const allowed: Record<string, string[]> = { persons: ['name', 'email', 'phone', 'notes', 'custom_fields'], organizations: ['name', 'address', 'notes', 'custom_fields'], deals: ['title', 'notes', 'custom_fields'] };
  const fields: string[] = [];
  for (const field of input.fields ?? []) { if (!allowed[input.objectType]!.includes(field)) throw Error(`search field ${JSON.stringify(field)} is not searchable for ${input.objectType}`); if (!fields.includes(field)) fields.push(field); }
  if (fields.length) query.set('fields', fields.join(','));
  if (input.exactMatch) query.set('exact_match', 'true');
  if (input.organizationId) { if (input.objectType === 'organizations') throw Error('organizationId filters only persons and deals searches'); requireId(input.organizationId, 'organizationId'); query.set('organization_id', input.organizationId); }
  if (input.objectType !== 'deals' && (input.personId || input.statuses?.length)) throw Error('personId and statuses filter only deals searches');
  if (input.personId) { requireId(input.personId, 'personId'); query.set('person_id', input.personId); }
  const statuses = joinStatuses(input.statuses, false); if (statuses) query.set('status', statuses);
  query.set('limit', String(pageLimit(input.limit))); requireCursor(input.cursor); if (input.cursor) query.set('cursor', input.cursor);
  return query;
}

function listQuery(input: ListObjectsInput): URLSearchParams {
  requireObjectType(input.objectType);
  const query = new URLSearchParams();
  if (input.updatedSince) query.set('updated_since', goTimeSeconds(input.updatedSince));
  if (input.updatedUntil) { if (input.updatedSince && Date.parse(input.updatedUntil) <= Date.parse(input.updatedSince)) throw Error('updatedUntil must be after updatedSince'); query.set('updated_until', goTimeSeconds(input.updatedUntil)); }
  const filters: Array<[string, string, string | undefined, boolean]> = [
    ['ownerId', 'owner_id', input.ownerId, true], ['organizationId', 'org_id', input.organizationId, input.objectType !== 'organizations'],
    ['personId', 'person_id', input.personId, input.objectType === 'deals'], ['pipelineId', 'pipeline_id', input.pipelineId, input.objectType === 'deals'], ['stageId', 'stage_id', input.stageId, input.objectType === 'deals'],
  ];
  for (const [name, key, value, supported] of filters) { if (!value) continue; if (!supported) throw Error(`${name} does not filter ${input.objectType}`); requireId(value, name); query.set(key, value); }
  if (input.statuses?.length && input.objectType !== 'deals') throw Error('statuses filter only deals');
  const statuses = joinStatuses(input.statuses, true); if (statuses) query.set('status', statuses);
  if ((input.customFieldKeys?.length ?? 0) > 15) throw Error('at most 15 custom field keys can be requested');
  for (const key of input.customFieldKeys ?? []) if (!customKeyPattern.test(key)) throw Error("a custom field key must be Pipedrive's 40-character lowercase hexadecimal key");
  if (input.customFieldKeys?.length) query.set('custom_fields', input.customFieldKeys.join(','));
  const sort = input.sortBy || (input.updatedSince ? 'update_time' : ''); if (sort) { if (!['id', 'update_time', 'add_time'].includes(sort)) throw Error('sortBy must be id, update_time, or add_time'); query.set('sort_by', sort); }
  if (input.sortDirection) { if (!['asc', 'desc'].includes(input.sortDirection)) throw Error('sortDirection must be asc or desc'); query.set('sort_direction', input.sortDirection); }
  query.set('limit', String(pageLimit(input.limit))); requireCursor(input.cursor); if (input.cursor) query.set('cursor', input.cursor);
  return query;
}

function goTimeSeconds(value: string): string { const parsed = parseGoTime(value); if (!parsed) throw Error('invalid timestamp'); return parsed.date.toISOString().slice(0, 19) + 'Z'; }

function writeBody(fields: Record<string, unknown> | null | undefined, customFields: Record<string, unknown> | undefined, requireName: boolean, objectType = ''): Record<string, unknown> {
  const standard = fields ?? {}; const custom = customFields ?? {};
  if (Object.keys(standard).length + Object.keys(custom).length > 100) throw Error('at most 100 field values can be written');
  for (const [name, value] of Object.entries(standard)) { if (!standardFieldPattern.test(name) || ['id', 'custom_fields'].includes(name)) throw Error('a written field name is invalid; put custom fields in customFields'); requireFieldSize(name, value); }
  for (const [key, value] of Object.entries(custom)) { if (!customKeyPattern.test(key)) throw Error("a custom field key must be Pipedrive's 40-character lowercase hexadecimal key"); requireFieldSize(key, value); }
  if (requireName) { const name = standard[objectType === 'deals' ? 'title' : 'name']; if (typeof name !== 'string' || !name.trim()) throw Error(`field ${objectType === 'deals' ? 'title' : 'name'} must be a non-blank JSON string`); }
  return { ...standard, ...(Object.keys(custom).length ? { custom_fields: custom } : {}) };
}
function requireFieldSize(name: string, value: unknown): void { if (value === undefined || Buffer.byteLength(JSON.stringify(value)) > 65_536) throw Error(`field ${name} value exceeds 65536 bytes`); }

function upsertBodies(input: UpsertObjectInput): { createBody: Record<string, unknown>; updateBody: Record<string, unknown>; identityField: string } {
  const updateBody = writeBody(input.fields, input.customFields, false);
  const fields = { ...(input.fields ?? {}) };
  let identityField: string;
  if (input.objectType === 'persons') {
    if (input.idProperty !== 'email') throw Error('a persons upsert is identified by idProperty email');
    if (!isPlainEmail(input.idValue) || Buffer.byteLength(input.idValue) > 254) throw Error('idValue must be one plain email address');
    if ('emails' in fields) throw Error('fields cannot set emails; the connector writes idValue as the primary email');
    if (typeof fields.name !== 'string' || !fields.name.trim()) throw Error('field name must be a non-blank JSON string');
    fields.emails = [{ value: input.idValue, primary: true, label: 'work' }]; identityField = 'email';
  } else if (input.objectType === 'organizations') {
    if (input.idProperty !== 'name') throw Error('an organizations upsert is identified by idProperty name');
    if (!input.idValue || input.idValue.trim() !== input.idValue || [...input.idValue].length > 255) throw Error('idValue must be a name of 1 to 255 characters without surrounding spaces');
    if ('name' in fields) throw Error('fields cannot set name; the connector writes idValue as the organization name');
    fields.name = input.idValue; identityField = 'name';
  } else throw Error('upsert supports persons by email and organizations by name; create deals with createObject');
  return { createBody: writeBody(fields, input.customFields, false), updateBody, identityField };
}
function isPlainEmail(value: string): boolean {
  const at = value.indexOf('@');
  if (at < 1 || at !== value.lastIndexOf('@') || at === value.length - 1) return false;
  const local = value.slice(0, at); const domain = value.slice(at + 1);
  const atom = /^[A-Za-z0-9!#$%&'*+\-/=?^_`{|}~\u{80}-\u{10FFFF}]+$/u;
  if (!local.split('.').every(part => part && atom.test(part))) return false;
  if (domain.startsWith('[') && domain.endsWith(']')) return isIP(domain.slice(1, -1)) !== 0;
  return domain.split('.').every(part => part && atom.test(part));
}

function parseObject(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw Error('malformed response'); return value as Record<string, unknown>; }
function parseBody(response: NonNullable<PipedriveExchange['response']>): Record<string, unknown> {
  const rawJSON = (JSON as typeof JSON & { rawJSON(source: string): unknown }).rawJSON;
  const source = Buffer.from(response.body).toString('utf8');
  const parseWithSource = JSON.parse as (text: string, reviver: (key: string, value: unknown, context: { source?: string }) => unknown) => unknown;
  const envelope = parseObject(parseWithSource(source, (_key, value, context) => typeof value === 'number' ? rawJSON(context.source!) : value));
  if (envelope.success !== true || envelope.data === undefined || envelope.data === null) throw Error('malformed response');
  return envelope;
}
function parseId(value: unknown): string {
  if (value === null || value === undefined) return '';
  const raw = value && typeof value === 'object' && 'rawJSON' in value ? String(value.rawJSON) : typeof value === 'number' ? String(value) : '';
  if (!idPattern.test(raw) || BigInt(raw) > 9_223_372_036_854_775_807n) throw Error('invalid ID');
  return raw;
}
function text(value: unknown): string { return typeof value === 'string' ? value : ''; }
function timestamp(value: unknown): string | undefined { if (typeof value !== 'string') return undefined; const parsed = parseGoTime(value); if (!parsed) return undefined; const fraction = parsed.fraction.replace(/0+$/, ''); return parsed.date.toISOString().slice(0, 19) + (fraction ? `.${fraction}` : '') + 'Z'; }
function parseGoTime(value: string): { date: Date; fraction: string } | undefined {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{1,2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|([+-])(\d{2}):(\d{2}))$/.exec(value);
  if (!match) return undefined;
  const year = Number(match[1]); const month = Number(match[2]); const day = Number(match[3]);
  const hour = Number(match[4]); const minute = Number(match[5]); const second = Number(match[6]);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  const zoneHour = Number(match[10] ?? 0); const zoneMinute = Number(match[11] ?? 0);
  if (month < 1 || month > 12 || day < 1 || day > days[month - 1]! || hour > 23 || minute > 59 || second > 59 || zoneHour > 24 || zoneMinute > 60) return undefined;
  const offsetMinutes = match[8] === 'Z' ? 0 : (match[9] === '+' ? 1 : -1) * (zoneHour * 60 + zoneMinute);
  const date = new Date(0);
  date.setUTCFullYear(year, month - 1, day);
  date.setUTCHours(hour, minute - offsetMinutes, second, 0);
  if (Number.isNaN(date.getTime())) return undefined;
  return { date, fraction: (match[7] ?? '').slice(0, 9) };
}
function decodeRecordBody(objectType: string, response: NonNullable<PipedriveExchange['response']>): CRMObject { return decodeRecord(objectType, parseBody(response).data, false); }
function decodeMatchingRecord(objectType: string, id: string, result: PipedriveExchange): CRMObject { const object = decodeRecordBody(objectType, result.response!); if (object.id !== id) throw Error('mismatched ID'); return object; }
function decodePage(objectType: string, response: NonNullable<PipedriveExchange['response']>, partial: boolean): ObjectPage {
  const envelope = parseBody(response); const data = envelope.data;
  let records: unknown[];
  if (partial) { const items = parseObject(data).items; if (items !== undefined && items !== null && !Array.isArray(items)) throw Error('malformed search items'); records = (items as unknown[] | null | undefined ?? []).map(item => parseObject(item).item); }
  else { if (!Array.isArray(data)) throw Error('malformed page'); records = data; }
  const objects = records.map(item => decodeRecord(objectType, item, partial));
  const additional = envelope.additional_data;
  if (additional !== undefined && additional !== null && (typeof additional !== 'object' || Array.isArray(additional))) throw Error('invalid additional data');
  const next = additional ? (additional as Record<string, unknown>).next_cursor : undefined;
  if (next !== undefined && next !== null && typeof next !== 'string') throw Error('invalid next cursor');
  if (next) requireCursor(next);
  return { objectType, objects, ...(next ? { nextCursor: next } : {}) };
}
function decodeRecord(objectType: string, value: unknown, partial: boolean): CRMObject {
  const fields = { ...parseObject(value) };
  const id = parseId(fields.id); if (!id) throw Error('record has no ID');
  const customFields = !partial && fields.custom_fields !== undefined && fields.custom_fields !== null ? parseObject(fields.custom_fields) : undefined;
  if (!partial) delete fields.custom_fields;
  const object: CRMObject = { objectType, id, ...(partial ? { isPartial: true } : {}), fields, ...(customFields ? { customFields } : {}) };
  const name = text(fields[objectType === 'deals' ? 'title' : 'name']); if (name) object.name = name;
  if (partial) {
    for (const [wire, property] of [['owner', 'ownerId'], ['organization', 'organizationId'], ['person', 'personId'], ['stage', 'stageId']] as const) {
      const linked = fields[wire]; if (linked !== undefined && linked !== null) { const linkedId = parseId(parseObject(linked).id); if (linkedId) object[property] = linkedId; }
    }
    if (objectType === 'persons' && Array.isArray(fields.emails) && fields.emails.every(email => typeof email === 'string')) object.emails = fields.emails as string[];
  } else {
    for (const [wire, property] of [['owner_id', 'ownerId'], ['org_id', 'organizationId'], ['person_id', 'personId'], ['pipeline_id', 'pipelineId'], ['stage_id', 'stageId']] as const) {
      if (wire === 'org_id' && objectType === 'organizations' || ['person_id', 'pipeline_id', 'stage_id'].includes(wire) && objectType !== 'deals') continue;
      const linked = parseId(fields[wire]); if (linked) object[property] = linked;
    }
    if (objectType === 'persons' && Array.isArray(fields.emails) && fields.emails.every(entry => entry === null || typeof entry === 'object' && !Array.isArray(entry) && (entry.value === undefined || typeof entry.value === 'string') && (entry.primary === undefined || typeof entry.primary === 'boolean'))) {
      const emails = fields.emails.filter((entry): entry is { value?: string; primary?: boolean } => entry !== null);
      const values = [...emails.filter(entry => entry.primary).map(entry => entry.value ?? ''), ...emails.filter(entry => !entry.primary).map(entry => entry.value ?? '')].filter(Boolean);
      if (values.length) object.emails = values;
    }
    const add = timestamp(fields.add_time); if (add) object.addTime = add;
    const update = timestamp(fields.update_time); if (update) object.updateTime = update;
  }
  if (objectType === 'deals') { const status = text(fields.status); if (status) object.status = status; }
  return object;
}
