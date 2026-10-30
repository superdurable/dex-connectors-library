// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { inspect } from 'node:util';
import { ConnectionRecordSchema, CredentialEnvelopeSchema, type ConnectionRecord } from './connection_schema.js';
import { type Scope, scopePrefix } from './model.js';
import { ConflictError, ObjectNotFoundError, OutcomeUnknownError, type ObjectStore, type ProjectObject, digestBytes, encodeGoCompatibleJSON } from './storage.js';

/** Manifest connector ID and application-selected connection name. */
export interface ConnectionKey {
  /** Manifest connector ID. */ connectorId: string;
  /** Static application connection name. */ connectionName: string;
}

/** Validate a connection key and return its scoped object prefix. */
export function connectionPath(scope: Scope, key: ConnectionKey): string {
  if (!/^[a-z][a-z0-9-]{1,62}$/.test(key.connectorId) || !key.connectionName || key.connectionName.trim() !== key.connectionName || key.connectionName.length > 256 || /[\0\r\n]/.test(key.connectionName)) throw new TypeError('invalid project connection key');
  return `${scopePrefix(scope)}/connections/${key.connectorId}/${Buffer.from(key.connectionName).toString('base64url')}`;
}

/** Private connector credential material outside Dex Flow state. */
export class CredentialMaterial {
  /** Capture complete private credentials and optional access expiry. */
  constructor(readonly credentials: Record<string, unknown>, readonly expiresAt: Date | undefined, readonly authMethod: string) {}
  /** Reject accidental durable JSON serialization. */ toJSON(): never { throw new TypeError('credential material cannot be serialized'); }
  /** Redact private material in logs. */ [inspect.custom](): string { return 'CredentialMaterial([REDACTED])'; }
}

/** Safe authoritative connection metadata without private credential bytes. */
export interface Connection {
  /** Connection identity. */ key: ConnectionKey;
  /** Project scope. */ scope: Scope;
  /** Head revision. */ revision: number;
  /** Monotonic mutation fence. */ fence: number;
  /** Authorization status. */ status: ConnectionRecord['status'];
  /** Known access expiry. */ expiresAt?: Date;
  /** Manifest authorization method. */ authMethod: string;
}

/** Versioned connection heads and immutable credential objects. */
export class ConnectionStore {
  /** Bind one object store and validated project scope. */
  constructor(readonly objects: ObjectStore, readonly scope: Scope) {
    if (!objects) throw new TypeError('project connection object store is required');
    scopePrefix(scope);
  }

  /** Read safe connection metadata without credential material. */
  async readConnection(key: ConnectionKey): Promise<Connection> {
    const { record } = await this.readRecord(key);
    return this.#connection(key, record);
  }

  /** Read exact current private material from a READY connection. */
  async readCredentialMaterial(key: ConnectionKey): Promise<{ material: CredentialMaterial; connection: Connection }> {
    const { record } = await this.readRecord(key);
    const connection = this.#connection(key, record);
    if (record.status !== 'READY') throw new Error('project credential is unavailable');
    return { material: await this.readMaterial(key, record), connection };
  }

  /** Read and validate a current head for refresh admission. */
  async readRecord(key: ConnectionKey): Promise<{ record: ConnectionRecord; object: ProjectObject }> {
    const path = connectionPath(this.scope, key);
    const object = await this.objects.readObject(`${path}/head`);
    if (!object.version || !object.etag) throw new Error('project connection head lacks version identity');
    const parsed = ConnectionRecordSchema.safeParse(parsePrivateJSON(object.contents));
    if (!parsed.success) throw new Error('project connection identity is invalid');
    const record = parsed.data;
    if (record.scope.projectId !== this.scope.projectId || record.scope.kind !== this.scope.kind || record.connection.connectorId !== key.connectorId || record.connection.connectionName !== key.connectionName) throw new Error('project connection identity is invalid');
    if (record.status === 'REVOKED' && (record.credential || record.mutation)) throw new Error('revoked connection retains active material');
    if (['READY', 'REFRESHING'].includes(record.status) && !record.credential) throw new Error('project credential reference is absent');
    if (record.credential && (!record.credential.key.startsWith(`${path}/`) || !record.credential.version || !/^[a-f0-9]{64}$/.test(record.credential.digest))) throw new Error('project credential reference is invalid');
    if (record.status === 'REFRESHING' || record.status === 'AUTHORIZING') {
      const mutation = record.mutation;
      if (!mutation || mutation.kind !== record.status || mutation.fence !== record.fence || mutation.baseRevision !== record.revision - 1 || !/^[A-Za-z0-9_-]{1,128}$/.test(mutation.id) || !mutation.invocationId || mutation.resultKey !== `${path}/exchange-results/${mutation.id}`) throw new Error('credential exchange admission is invalid');
    } else if (record.mutation) throw new Error('inactive credential exchange admission is invalid');
    return { record, object };
  }

  /** Read private material by an already validated head. */
  async readMaterial(key: ConnectionKey, record: ConnectionRecord): Promise<CredentialMaterial> {
    const reference = record.credential;
    if (!reference) throw new Error('project credential reference is absent');
    const object = await this.objects.readObject(reference.key, reference.version);
    if (digestBytes(object.contents).slice(7) !== reference.digest) throw new Error('project credential integrity differs');
    const parsed = CredentialEnvelopeSchema.safeParse(parsePrivateJSON(object.contents));
    if (!parsed.success) throw new Error('project credential envelope violates its schema');
    const envelope = parsed.data;
    if (envelope.scope.projectId !== this.scope.projectId || envelope.scope.kind !== this.scope.kind || envelope.connection.connectorId !== key.connectorId || envelope.connection.connectionName !== key.connectionName || envelope.authMethod !== record.authMethod || !sameExpiry(envelope.expiresAt, record.expiresAt)) throw new Error('project credential identity differs');
    return new CredentialMaterial(envelope.credentials, parseExpiry(envelope.expiresAt), envelope.authMethod);
  }

  /** Fence prior work and publish one complete immutable credential version. */
  async replaceCredential(key: ConnectionKey, expectedRevision: number, material: CredentialMaterial): Promise<Connection> {
    if (!material.authMethod || !material.credentials || typeof material.credentials !== 'object' || Array.isArray(material.credentials)) throw new TypeError('invalid credential material');
    const path = connectionPath(this.scope, key);
    let previous: { record: ConnectionRecord; object: ProjectObject } | undefined;
    try { previous = await this.readRecord(key); }
    catch (error) { if (!(error instanceof ObjectNotFoundError && expectedRevision === 0)) throw error; }
    if ((previous?.record.revision ?? 0) !== expectedRevision) throw new ConflictError();
    const fence = (previous?.record.fence ?? 0) + 1;
    const envelope = credentialEnvelope(this.scope, key, fence, material);
    const contents = Buffer.from(encodeGoCompatibleJSON(envelope));
    const immutableKey = `${path}/credentials/${digestBytes(contents).slice(7)}`;
    let created: ProjectObject;
    try { created = await this.objects.createObject(immutableKey, contents); }
    catch (error) {
      if (!(error instanceof ConflictError || error instanceof OutcomeUnknownError)) throw error;
      created = await this.objects.readObject(immutableKey);
      if (!Buffer.from(created.contents).equals(contents)) throw new Error('immutable credential content differs');
    }
    const reference = { key: immutableKey, version: created.version, digest: digestBytes(contents).slice(7) };
    const record = connectionRecord(this.scope, key, expectedRevision + 1, fence, 'READY', material.expiresAt, material.authMethod, reference);
    await this.writeHead(key, previous?.object, record);
    return this.#connection(key, record);
  }

  /** Conditionally publish a new head and reconcile an unknown write outcome. */
  async writeHead(key: ConnectionKey, previous: ProjectObject | undefined, record: ConnectionRecord): Promise<ProjectObject> {
    const path = connectionPath(this.scope, key);
    const contents = Buffer.from(encodeGoCompatibleJSON(record));
    try {
      return previous ? await this.objects.compareAndSwapObject(`${path}/head`, previous.etag, contents) : await this.objects.createObject(`${path}/head`, contents);
    } catch (error) {
      if (!(error instanceof OutcomeUnknownError)) throw error;
      const observed = await this.objects.readObject(`${path}/head`);
      if (!Buffer.from(observed.contents).equals(contents)) throw error;
      return observed;
    }
  }

  #connection(key: ConnectionKey, record: ConnectionRecord): Connection {
    const expiresAt = parseExpiry(record.expiresAt);
    return { key, scope: this.scope, revision: record.revision, fence: record.fence, status: record.status, authMethod: record.authMethod, ...(expiresAt ? { expiresAt } : {}) };
  }
}

/** Build a Go-compatible private credential envelope. */
export function credentialEnvelope(scope: Scope, key: ConnectionKey, fence: number, material: CredentialMaterial, attemptId?: string): Record<string, unknown> {
  return { schemaVersion: 'connectors.dex.dev/project-connection/v1alpha2', scope, connection: key, fence, ...(attemptId ? { attemptId } : {}), credentials: material.credentials, ...(material.expiresAt ? { expiresAt: material.expiresAt.toISOString() } : {}), authMethod: material.authMethod };
}

/** Build a Go-compatible connection head. */
export function connectionRecord(scope: Scope, key: ConnectionKey, revision: number, fence: number, status: ConnectionRecord['status'], expiresAt: Date | undefined, authMethod: string, credential?: ConnectionRecord['credential'], mutation?: ConnectionRecord['mutation']): ConnectionRecord {
  return ConnectionRecordSchema.parse({ schemaVersion: 'connectors.dex.dev/project-connection/v1alpha2', connection: key, scope, revision, fence, status, ...(expiresAt ? { expiresAt: expiresAt.toISOString() } : {}), authMethod, ...(credential ? { credential } : {}), ...(mutation ? { mutation } : {}) });
}

function parsePrivateJSON(contents: Uint8Array): unknown {
  try { return JSON.parse(Buffer.from(contents).toString('utf8')) as unknown; }
  catch { throw new Error('project credential is not valid JSON'); }
}

function parseExpiry(value: string | null | undefined): Date | undefined { return value ? new Date(value) : undefined; }
function sameExpiry(left: string | null | undefined, right: string | null | undefined): boolean { return left && right ? rfc3339Nanoseconds(left) === rfc3339Nanoseconds(right) : !left && !right; }

function rfc3339Nanoseconds(value: string): bigint {
  const parts = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!parts) throw new Error('project credential expiry is invalid');
  const milliseconds = Date.parse(`${parts[1]}${parts[3]}`);
  if (!Number.isFinite(milliseconds)) throw new Error('project credential expiry is invalid');
  return BigInt(milliseconds) * 1_000_000n + BigInt((parts[2] ?? '').padEnd(9, '0') || '0');
}
