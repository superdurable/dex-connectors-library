// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { setTimeout as wait } from 'node:timers/promises';
import type { Call, CredentialProvider as CredentialProviderContract, CredentialRefreshDriver, CredentialRefreshResult, CredentialRefreshState, RefreshingCredentialProvider as RefreshingCredentialProviderContract, RejectedCredentialRefreshingProvider } from '../credentials.js';
import { ReauthorizationRequiredError } from '../errors.js';
import { OperationRefSchema } from '../types.js';
import { CredentialEnvelopeSchema, type ConnectionRecord } from './connection_schema.js';
import { RejectedCredentialNotRefreshedError } from './broker.js';
import { ConnectionStore, CredentialMaterial, connectionPath, connectionRecord, credentialEnvelope, type ConnectionKey } from './connections.js';
import { ConflictError, ObjectNotFoundError, OutcomeUnknownError, digestBytes, encodeGoCompatibleJSON, type ProjectObject } from './storage.js';

/** Connector-owned validation of complete private credential JSON. */
export type CredentialDecoder<C> = (document: Record<string, unknown>) => C;
/** Connector-owned encoding of complete replacement material. */
export type CredentialEncoder<C> = (credentials: C) => Record<string, unknown>;
/** Decode broker material that omits secrets retained by the shared connection host. */
export type SharedCredentialDecoder<C> = (document: Record<string, unknown>) => C;

/** Another invocation owns provider dispatch for this credential generation. */
export class ExchangePendingError extends Error { constructor() { super('credential exchange is pending'); this.name = 'ExchangePendingError'; } }
/** Refresh failed without proving the prior authorization invalid. */
export class RefreshFailedError extends Error { constructor(message = 'project credential refresh failed; current authorization is kept') { super(message); this.name = 'RefreshFailedError'; } }

interface Admission {
  key: ConnectionKey;
  attemptId: string;
  fence: number;
  deadline: Date;
  resultKey: string;
}

/** Read a non-refreshing project credential on every connector invocation. */
export class CredentialProvider<C> implements CredentialProviderContract<C> {
  /** Bind one validated project connection and a credential decoder. */
  constructor(protected readonly store: ConnectionStore, protected readonly key: ConnectionKey, protected readonly decode: CredentialDecoder<C>, protected readonly decodeShared?: SharedCredentialDecoder<C>) {
    if (!store || !decode) throw new TypeError('project credential store and decoder are required');
    connectionPath(store.scope, key);
  }

  /** Read current private material without refreshing known-expired credentials. */
  async resolve(call: Call, signal?: AbortSignal): Promise<C> {
    this.validateCall(call);
    const effectiveSignal = signal ?? call.context.cancellationSignal;
    throwIfAborted(effectiveSignal);
    if (this.store.shared?.has(this.key)) return this.decodeSharedMaterial((await this.store.shared.read(this.key, call.id, effectiveSignal)).credential);
    const { material, connection } = await this.store.readCredentialMaterial(this.key);
    if (connection.expiresAt && connection.expiresAt.getTime() <= Date.now()) throw new Error('expired project credential requires refresh');
    return this.decodeMaterial(material);
  }

  /** Read current material under an explicit cancellation signal. */
  async resolveContext(call: Call, signal: AbortSignal): Promise<C> { return this.resolve(call, signal); }

  /** Reject a call aimed at another configured connection. */
  protected validateCall(call: Call): void {
    if (!call || call.operation.connectorId !== this.key.connectorId || call.connection.name !== this.key.connectionName) throw new TypeError('connector call differs from configured project connection');
    OperationRefSchema.parse(call.operation);
  }

  /** Decode private material without leaking invalid values in errors. */
  protected decodeMaterial(material: CredentialMaterial): C {
    try { return this.decode(material.credentials); }
    catch { throw new TypeError('project credential decoding failed'); }
  }

  /** Decode a broker response with the connector's shared credential rules. */
  protected decodeSharedMaterial(document: Record<string, unknown>): C {
    try { return (this.decodeShared ?? this.decode)(document); }
    catch { throw new TypeError(this.decodeShared ? 'shared project credential decoding failed' : 'project credential decoding failed: this connector version does not support shared credentials'); }
  }
}

/** Refresh and persist project credentials once per generation across replicas. */
export class RefreshingCredentialProvider<C> extends CredentialProvider<C> implements RefreshingCredentialProviderContract<C>, RejectedCredentialRefreshingProvider<C> {
  readonly #encode: CredentialEncoder<C>;

  /** Bind a complete replacement encoder in addition to the decoder. */
  constructor(store: ConnectionStore, key: ConnectionKey, decode: CredentialDecoder<C>, encode: CredentialEncoder<C>, decodeShared?: SharedCredentialDecoder<C>) {
    super(store, key, decode, decodeShared);
    if (!encode) throw new TypeError('project credential encoder is required');
    this.#encode = encode;
  }

  /** Admit a durable refresh before one provider request; observers recover its result. */
  async resolveWithRefresh(call: Call, driver: CredentialRefreshDriver<C>, signal?: AbortSignal): Promise<C> {
    this.validateCall(call);
    if (!driver) throw new TypeError('credential refresh driver is required');
    const effectiveSignal = signal ?? call.context.cancellationSignal;
    if (this.store.shared?.has(this.key)) return this.decodeSharedMaterial((await this.store.shared.read(this.key, call.id, effectiveSignal)).credential);
    for (;;) {
      throwIfAborted(effectiveSignal);
      const { record, object } = await this.store.readRecord(this.key);
      if (record.status === 'REAUTHORIZATION_REQUIRED' || record.status === 'REVOKED') throw new ReauthorizationRequiredError();
      if (record.status === 'AUTHORIZING') throw new ExchangePendingError();
      if (record.status === 'REFRESHING') {
        await this.#recoverOrWait(record, effectiveSignal);
        continue;
      }
      if (record.status !== 'READY') throw new TypeError('project credential status is invalid');
      const material = await this.store.readMaterial(this.key, record);
      const credentials = this.decodeMaterial(material);
      const now = new Date();
      const state: CredentialRefreshState<C> = { credentials, now, ...(material.expiresAt ? { expiresAt: material.expiresAt } : {}) };
      if ((!material.expiresAt || material.expiresAt.getTime() > now.getTime()) && !driver.refreshRequired(state)) return credentials;
      let admission: Admission;
      try { admission = await this.#admitRefresh(record, object, now); }
      catch (error) { if (error instanceof ConflictError) continue; throw error; }
      return this.#refreshAdmitted(admission, material, state, driver, effectiveSignal);
    }
  }

  /** Force refresh only when stored expiry proves the rejected token expired. */
  async resolveAfterRejection(call: Call, driver: CredentialRefreshDriver<C>, signal?: AbortSignal): Promise<C> {
    this.validateCall(call);
    if (!driver) throw new TypeError('credential refresh driver is required');
    if (this.store.shared?.has(this.key)) return this.decodeSharedMaterial((await this.store.shared.readAfterRejection(this.key, call.id, signal ?? call.context.cancellationSignal)).credential);
    const connection = await this.store.readConnection(this.key);
    if (!connection.expiresAt || connection.expiresAt.getTime() > Date.now()) throw new RejectedCredentialNotRefreshedError();
    return this.resolveWithRefresh(call, driver, signal);
  }

  async #admitRefresh(record: ConnectionRecord, object: ProjectObject, now: Date): Promise<Admission> {
    const attemptId = randomUUID().replaceAll('-', '');
    const deadline = new Date(now.getTime() + 40_000);
    const resultKey = `${connectionPath(this.store.scope, this.key)}/exchange-results/${attemptId}`;
    const mutation = { kind: 'REFRESHING', invocationId: randomUUID().replaceAll('-', ''), id: attemptId, fence: record.fence + 1, baseRevision: record.revision, deadline: deadline.toISOString(), resultKey };
    const updated = connectionRecord(this.store.scope, this.key, record.revision + 1, record.fence + 1, 'REFRESHING', parseExpiry(record.expiresAt), record.authMethod, record.credential, mutation);
    await this.store.writeHead(this.key, object, updated);
    return { key: this.key, attemptId, fence: updated.fence, deadline, resultKey };
  }

  async #refreshAdmitted(admission: Admission, prior: CredentialMaterial, state: CredentialRefreshState<C>, driver: CredentialRefreshDriver<C>, signal?: AbortSignal): Promise<C> {
    let result: CredentialRefreshResult<C>;
    try { result = await refreshWithinTimeout(driver, state, signal); }
    catch (error) {
      if (error instanceof ReauthorizationRequiredError) {
        await this.#finishRefresh(admission, 'REAUTHORIZATION_REQUIRED');
        throw error;
      }
      await this.#finishRefresh(admission, 'READY');
      throw new RefreshFailedError();
    }
    if (!(result.expiresAt instanceof Date) || !Number.isFinite(result.expiresAt.getTime()) || result.expiresAt.getTime() <= Date.now()) {
      await this.#finishRefresh(admission, 'READY');
      throw new RefreshFailedError('refreshed credential has no future expiry');
    }
    let encoded: Record<string, unknown>;
    try {
      encoded = this.#encode(result.credentials);
      if (!encoded || typeof encoded !== 'object' || Array.isArray(encoded) || Object.keys(encoded).length === 0) throw new TypeError();
    } catch {
      await this.#finishRefresh(admission, 'READY');
      throw new RefreshFailedError('refreshed credential encoding failed');
    }
    await this.#commitRefresh(admission, new CredentialMaterial(encoded, result.expiresAt, prior.authMethod));
    return result.credentials;
  }

  async #recoverOrWait(record: ConnectionRecord, signal?: AbortSignal): Promise<void> {
    const mutation = record.mutation;
    if (!mutation) throw new TypeError('credential exchange admission is absent');
    const admission: Admission = { key: this.key, attemptId: mutation.id, fence: record.fence, deadline: new Date(mutation.deadline), resultKey: mutation.resultKey };
    let result: ProjectObject;
    try { result = await this.store.objects.readObject(admission.resultKey); }
    catch (error) {
      if (!(error instanceof ObjectNotFoundError)) throw error;
      if (Date.now() < admission.deadline.getTime()) { await wait(100, undefined, signal ? { signal } : undefined); return; }
      await this.#finishRefresh(admission, 'READY');
      return;
    }
    await this.#publishResult(admission, result);
  }

  async #commitRefresh(admission: Admission, material: CredentialMaterial): Promise<void> {
    const envelope = credentialEnvelope(this.store.scope, this.key, admission.fence, material, admission.attemptId);
    const contents = Buffer.from(encodeGoCompatibleJSON(envelope));
    let result: ProjectObject;
    try { result = await this.store.objects.createObject(admission.resultKey, contents); }
    catch (error) {
      if (!(error instanceof ConflictError || error instanceof OutcomeUnknownError)) throw error;
      result = await this.store.objects.readObject(admission.resultKey);
      if (!Buffer.from(result.contents).equals(contents)) throw new Error('immutable credential result differs');
    }
    await this.#publishResult(admission, result);
  }

  async #publishResult(admission: Admission, result: ProjectObject): Promise<void> {
    const { record, object } = await this.store.readRecord(this.key);
    if (record.status === 'READY' && record.credential?.key === result.key) return;
    requireAdmission(record, admission);
    const parsed = CredentialEnvelopeSchema.safeParse(parsePrivateJSON(result.contents));
    if (!parsed.success) throw new TypeError('credential exchange result is invalid');
    const envelope = parsed.data;
    if (envelope.scope.projectId !== this.store.scope.projectId || envelope.scope.kind !== this.store.scope.kind || envelope.connection.connectorId !== this.key.connectorId || envelope.connection.connectionName !== this.key.connectionName || envelope.fence !== admission.fence || envelope.attemptId !== admission.attemptId) throw new TypeError('credential exchange result identity is invalid');
    const reference = { key: result.key, version: result.version, digest: digestBytes(result.contents).slice(7) };
    const updated = connectionRecord(this.store.scope, this.key, record.revision + 1, record.fence, 'READY', parseExpiry(envelope.expiresAt), envelope.authMethod, reference);
    try { await this.store.writeHead(this.key, object, updated); }
    catch (error) {
      if (!(error instanceof ConflictError)) throw error;
      const observed = await this.store.readRecord(this.key);
      if (observed.record.status !== 'READY' || observed.record.credential?.key !== result.key) throw error;
    }
  }

  async #finishRefresh(admission: Admission, status: 'READY' | 'REAUTHORIZATION_REQUIRED'): Promise<void> {
    const { record, object } = await this.store.readRecord(this.key);
    requireAdmission(record, admission);
    const updated = connectionRecord(this.store.scope, this.key, record.revision + 1, record.fence + 1, status, parseExpiry(record.expiresAt), record.authMethod, record.credential);
    await this.store.writeHead(this.key, object, updated);
  }
}

/** Bind an API-key or other non-refreshing credential to one project connection. */
export function newCredentialProvider<C>(store: ConnectionStore, key: ConnectionKey, decode: CredentialDecoder<C>, decodeShared?: SharedCredentialDecoder<C>): CredentialProvider<C> { return new CredentialProvider(store, key, decode, decodeShared); }
/** Bind a renewable credential and complete replacement encoder to one connection. */
export function newRefreshingCredentialProvider<C>(store: ConnectionStore, key: ConnectionKey, decode: CredentialDecoder<C>, encode: CredentialEncoder<C>, decodeShared?: SharedCredentialDecoder<C>): RefreshingCredentialProvider<C> { return new RefreshingCredentialProvider(store, key, decode, encode, decodeShared); }

function requireAdmission(record: ConnectionRecord, admission: Admission): void {
  if (record.status !== 'REFRESHING' || record.fence !== admission.fence || record.mutation?.id !== admission.attemptId) throw new ConflictError();
}

function parsePrivateJSON(contents: Uint8Array): unknown {
  try { return JSON.parse(Buffer.from(contents).toString('utf8')) as unknown; }
  catch { throw new TypeError('project credential is not valid JSON'); }
}

function parseExpiry(value: string | null | undefined): Date | undefined { return value ? new Date(value) : undefined; }

function throwIfAborted(signal?: AbortSignal): void { if (signal?.aborted) throw signal.reason instanceof Error ? signal.reason : new Error('credential operation cancelled'); }

async function refreshWithinTimeout<C>(driver: CredentialRefreshDriver<C>, state: CredentialRefreshState<C>, signal?: AbortSignal): Promise<CredentialRefreshResult<C>> {
  const timeoutController = new AbortController();
  const timer = setTimeout(() => timeoutController.abort(new Error('credential refresh timed out')), 30_000);
  const refreshSignal = signal ? AbortSignal.any([signal, timeoutController.signal]) : timeoutController.signal;
  let rejectInterruption!: (error: Error) => void;
  const interrupted = new Promise<never>((_resolve, reject) => { rejectInterruption = reject; });
  const onAbort = () => rejectInterruption(new Error('credential refresh interrupted'));
  if (refreshSignal.aborted) onAbort();
  else refreshSignal.addEventListener('abort', onAbort, { once: true });
  try {
    return await Promise.race([driver.refresh(state, refreshSignal), interrupted]);
  } finally {
    clearTimeout(timer);
    refreshSignal.removeEventListener('abort', onAbort);
  }
}
