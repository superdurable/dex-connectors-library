// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { inspect } from 'node:util';
import { GetBucketVersioningCommand, GetObjectCommand, PutObjectCommand, S3Client } from '@aws-sdk/client-s3';
import { readBoundedBody } from '../providerhttp/index.js';
import { ConfigurationSchemaVersion, ConfigurationSchema, type Configuration, type Scope, type SnapshotRef, scopePrefix } from './model.js';

/** Object is absent at the requested key or exact version. */
export class ObjectNotFoundError extends Error { constructor() { super('project configuration object is absent'); this.name = 'ObjectNotFoundError'; } }
/** Conditional write lost to another revision. */
export class ConflictError extends Error { constructor() { super('project configuration revision changed'); this.name = 'ConflictError'; } }
/** Write acceptance is unknown and must be reconciled before another mutation. */
export class OutcomeUnknownError extends Error { constructor() { super('project configuration write outcome is unknown'); this.name = 'OutcomeUnknownError'; } }

/** Private object with exact version and compare-and-swap identity. */
export class ProjectObject {
  readonly #contents: Uint8Array;
  /** Keep private bytes outside Node inspection; key, version, and ETag identify the exact object. */
  constructor(readonly key: string, readonly version: string, readonly etag: string, contents: Uint8Array) { this.#contents = contents; }
  /** Exact private bytes; callers must not log or persist them. */
  get contents(): Uint8Array { return this.#contents; }
  /** Never format private contents. */
  toString(): string { return `ProjectObject{${this.key} version=${this.version}}`; }
  /** Keep Node inspection and console output secret-safe. */
  [inspect.custom](): string { return this.toString(); }
  /** Reject accidental JSON transport. */
  toJSON(): never { throw new TypeError('private project objects cannot be serialized'); }
}

/** Strongly consistent versioned object store with conditional writes. */
export interface ObjectStore {
  /** Read current or exact object version. */
  readObject(key: string, version?: string): Promise<ProjectObject>;
  /** Create only when the key is absent. */
  createObject(key: string, contents: Uint8Array): Promise<ProjectObject>;
  /** Replace only if current ETag equals the expected value. */
  compareAndSwapObject(key: string, expectedETag: string, contents: Uint8Array): Promise<ProjectObject>;
}

/** Process-local versioned store for Dex Web preview and deterministic tests. */
export class InMemoryObjectStore implements ObjectStore {
  readonly #versions = new Map<string, ProjectObject[]>();

  /** Read the latest object or an exact immutable version. */
  async readObject(key: string, version = ''): Promise<ProjectObject> {
    validateObjectKey(key);
    const versions = this.#versions.get(key) ?? [];
    const found = version ? versions.find(object => object.version === version) : versions.at(-1);
    if (!found) throw new ObjectNotFoundError();
    return found;
  }

  /** Create only when no object exists under this key. */
  async createObject(key: string, contents: Uint8Array): Promise<ProjectObject> {
    validateObjectKey(key);
    validateContents(contents);
    if (this.#versions.has(key)) throw new ConflictError();
    return this.#write(key, contents);
  }

  /** Compare and swap the latest object by ETag. */
  async compareAndSwapObject(key: string, expectedETag: string, contents: Uint8Array): Promise<ProjectObject> {
    validateObjectKey(key);
    validateContents(contents);
    const current = (this.#versions.get(key) ?? []).at(-1);
    if (!current || current.etag !== expectedETag) throw new ConflictError();
    return this.#write(key, contents);
  }

  #write(key: string, contents: Uint8Array): ProjectObject {
    const version = String((this.#versions.get(key)?.length ?? 0) + 1);
    const object = new ProjectObject(key, version, `${digestBytes(contents).slice(7)}-${version}`, new Uint8Array(contents));
    this.#versions.set(key, [...(this.#versions.get(key) ?? []), object]);
    return object;
  }
}

/** Fixed S3 storage boundary with exact encryption configuration. */
export interface S3StoreConfig {
  /** Authenticated S3 client owned by the application. */ client: S3Client;
  /** Existing private, versioned bucket. */ bucket: string;
  /** Optional trusted key prefix. */ prefix?: string;
  /** Exact KMS key ARN. */ kmsKeyId?: string;
  /** Isolated local fixture only. */ allowUnencrypted?: boolean;
}

/** S3 implementation using If-Match and If-None-Match without write retries. */
export class S3ObjectStore implements ObjectStore {
  private constructor(private readonly configuration: S3StoreConfig, private readonly prefix: string) {}

  /** Verify bucket versioning before any application request. */
  static async create(configuration: S3StoreConfig): Promise<S3ObjectStore> {
    if (!configuration.client || !configuration.bucket || /[/\\\0]/.test(configuration.bucket)) throw new TypeError('versioned project storage is required');
    const hasExactKmsKey = /^arn:[a-z0-9-]+:kms:[a-z0-9-]+:[0-9]{12}:key\/[a-zA-Z0-9-]+$/.test(configuration.kmsKeyId ?? '');
    if (!hasExactKmsKey && !(configuration.allowUnencrypted && !configuration.kmsKeyId)) throw new TypeError('exact hosted KMS key ARN is required');
    const prefix = configuration.prefix?.replace(/\/$/, '') ?? '';
    if (prefix) validateObjectKey(prefix);
    if (await configuration.client.config.maxAttempts() !== 1) throw new TypeError('S3 client must disable automatic write retries with maxAttempts: 1');
    const versioning = await configuration.client.send(new GetBucketVersioningCommand({ Bucket: configuration.bucket }));
    if (versioning.Status !== 'Enabled') throw new Error('project object bucket versioning must be enabled');
    return new S3ObjectStore(configuration, prefix ? `${prefix}/` : '');
  }

  /** Read bounded private bytes and validate exact version and KMS key. */
  async readObject(key: string, version = ''): Promise<ProjectObject> {
    validateObjectKey(key);
    let result;
    try { result = await this.configuration.client.send(new GetObjectCommand({ Bucket: this.configuration.bucket, Key: this.prefix + key, ...(version ? { VersionId: version } : {}) })); }
    catch (error: unknown) { throw classifyStorageError(error, false); }
    if (!result.VersionId || result.VersionId === 'null' || !result.ETag || (version && result.VersionId !== version)) throw new Error('project object lacks exact version identity');
    if (this.configuration.kmsKeyId && (result.ServerSideEncryption !== 'aws:kms' || result.SSEKMSKeyId !== this.configuration.kmsKeyId)) throw new Error('project object encryption boundary differs');
    if (!result.Body) throw new Error('project object body is unavailable');
    const contents = await readBoundedBody(result.Body as AsyncIterable<Uint8Array>, 4 << 20);
    validateContents(contents);
    return new ProjectObject(key, result.VersionId, result.ETag, contents);
  }

  /** Write once with If-None-Match. */
  async createObject(key: string, contents: Uint8Array): Promise<ProjectObject> { return this.writeObject(key, '', contents); }

  /** Write once with If-Match; ambiguous outcomes stay unknown. */
  async compareAndSwapObject(key: string, expectedETag: string, contents: Uint8Array): Promise<ProjectObject> {
    if (!expectedETag) throw new TypeError('expected ETag is required');
    return this.writeObject(key, expectedETag, contents);
  }

  private async writeObject(key: string, etag: string, contents: Uint8Array): Promise<ProjectObject> {
    validateObjectKey(key);
    validateContents(contents);
    let result;
    try {
      result = await this.configuration.client.send(new PutObjectCommand({
        Bucket: this.configuration.bucket, Key: this.prefix + key, Body: contents, ContentType: 'application/json',
        ...(etag ? { IfMatch: etag } : { IfNoneMatch: '*' }),
        ...(this.configuration.kmsKeyId ? { ServerSideEncryption: 'aws:kms' as const, SSEKMSKeyId: this.configuration.kmsKeyId } : {}),
      }));
    } catch (error: unknown) { throw classifyStorageError(error, true); }
    if (!result.VersionId || result.VersionId === 'null' || !result.ETag) throw new OutcomeUnknownError();
    return new ProjectObject(key, result.VersionId, result.ETag, contents);
  }
}

/** Configuration CAS and immutable snapshot reader. */
export class ConfigurationStore {
  readonly #key: string;
  /** Bind a private object store to one validated project scope. */
  constructor(private readonly objects: ObjectStore, private readonly scope: Scope) {
    if (!objects) throw new TypeError('configuration object store is required');
    this.#key = `${scopePrefix(scope)}/configuration/head`;
  }

  /** Read and validate current ordinary configuration. */
  async readConfiguration(): Promise<{ configuration: Configuration; object: ProjectObject }> {
    const object = await this.objects.readObject(this.#key);
    return { configuration: this.decodeConfiguration(object), object };
  }

  /** Conditionally replace ordinary settings using an expected revision. */
  async writeConfiguration(expectedRevision: number, document: Omit<Configuration, 'schemaVersion' | 'scope' | 'revision'>): Promise<Configuration> {
    let previous: Configuration | undefined;
    let priorObject: ProjectObject | undefined;
    try {
      const current = await this.readConfiguration();
      previous = current.configuration;
      priorObject = current.object;
    } catch (error: unknown) {
      if (!(error instanceof ObjectNotFoundError && expectedRevision === 0)) throw error;
    }
    if ((previous?.revision ?? 0) !== expectedRevision) throw new ConflictError();
    const schemaVersion = document.credentialBroker || document.connections.some(connection => connection.sharedConnectionId) ? ConfigurationSchemaVersion : 'connectors.dex.dev/project-configuration/v1alpha2';
    const configuration = ConfigurationSchema.parse({ schemaVersion, scope: this.scope, revision: expectedRevision + 1, ...document });
    const contents = Buffer.from(encodeGoCompatibleJSON(configuration));
    try {
      if (!priorObject) await this.objects.createObject(this.#key, contents);
      else await this.objects.compareAndSwapObject(this.#key, priorObject.etag, contents);
    } catch (error: unknown) {
      if (!(error instanceof OutcomeUnknownError)) throw error;
      try {
        const observed = await this.objects.readObject(this.#key);
        if (!Buffer.from(observed.contents).equals(contents)) throw error;
      } catch { throw error; }
    }
    return configuration;
  }

  /** Read an exact pinned snapshot after checking its digest. */
  async readSnapshot(reference: SnapshotRef): Promise<Configuration> {
    if (reference.key !== this.#key || !reference.version || reference.version === 'null' || reference.mediaType !== 'application/json') throw new TypeError('invalid configuration snapshot reference');
    const object = await this.objects.readObject(reference.key, reference.version);
    if (digestBytes(object.contents) !== reference.digest) throw new Error('configuration snapshot digest differs');
    return this.decodeConfiguration(object);
  }

  /** Pin the current version when its revision is still expected. */
  async freezeConfiguration(expectedRevision: number): Promise<SnapshotRef> {
    const { configuration, object } = await this.readConfiguration();
    if (configuration.revision !== expectedRevision) throw new ConflictError();
    return { key: object.key, version: object.version, digest: digestBytes(object.contents), mediaType: 'application/json' };
  }

  private decodeConfiguration(object: ProjectObject): Configuration {
    if (object.key !== this.#key || !object.version || !object.etag) throw new Error('configuration document identity is invalid');
    let decoded: unknown;
    try { decoded = JSON.parse(Buffer.from(object.contents).toString('utf8')) as unknown; }
    catch { throw new Error('configuration document is not valid JSON'); }
    const configuration = ConfigurationSchema.parse(decoded);
    if (configuration.scope.projectId !== this.scope.projectId || configuration.scope.kind !== this.scope.kind) throw new Error('configuration scope differs');
    return configuration;
  }
}

/** Validate a relative object key before accessing private storage. */
export function validateObjectKey(key: string): void {
  if (!key || key.length > 1024 || key.startsWith('/') || /[\\\0\r\n]/.test(key) || key.split('/').some(segment => !segment || segment === '.' || segment === '..')) throw new TypeError('invalid project object key');
}

/** Return the scoped Go-compatible head key for a named connection. */
export function connectionHeadKey(scope: Scope, connectorId: string, connectionName: string): string {
  if (!/^[a-z][a-z0-9-]{1,62}$/.test(connectorId) || !connectionName.trim()) throw new TypeError('connection identity is invalid');
  return `${scopePrefix(scope)}/connections/${connectorId}/${Buffer.from(connectionName).toString('base64url')}/head`;
}

/** SHA-256 digest in Go projectconfig wire format. */
export function digestBytes(contents: Uint8Array): string { return `sha256:${createHash('sha256').update(contents).digest('hex')}`; }

/** Encode Go-compatible compact JSON before hashing. Known structs use field-order heuristics; raw maps sort UTF-8 keys, unlike Go's preserved RawMessage order. Unpaired surrogates retain JavaScript escaping. */
export function encodeGoCompatibleJSON(document: unknown): string {
  if (document === null || typeof document !== 'object') throw new TypeError('project document must be a JSON object');
  return encodeCanonicalValue(document);
}

function encodeCanonicalValue(value: unknown, context: 'structured' | 'map' | 'environment' = 'structured'): string {
  if (typeof value === 'number' && !Number.isFinite(value)) throw new TypeError('project document contains a non-finite number');
  if (value === null || typeof value === 'boolean' || typeof value === 'number') return JSON.stringify(value);
  if (typeof value === 'string') return JSON.stringify(value).replace(/[<>&\u2028\u2029]/g, character => {
    const escapes: Record<string, string> = { '<': '\\u003c', '>': '\\u003e', '&': '\\u0026', '\u2028': '\\u2028', '\u2029': '\\u2029' };
    return escapes[character] ?? character;
  });
  if (Array.isArray(value)) return `[${value.map(item => encodeCanonicalValue(item, context)).join(',')}]`;
  if (typeof value !== 'object') throw new TypeError('project document contains a non-JSON value');
  const object = value as Record<string, unknown>;
  const isStructured = context === 'structured';
  const keys = isStructured ? canonicalFieldOrder(object) : Object.keys(object).sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right)));
  return `{${keys.filter(key => object[key] !== undefined && (!isStructured || !shouldOmitGoField(object, key))).map(key => {
    const childContext = context === 'environment' ? 'structured' : context === 'map' ? 'map' : key === 'environment' ? 'environment' : ['configuration', 'credentials', 'event'].includes(key) ? 'map' : 'structured';
    const fieldValue = isStructured && ['expiresAt', 'deadline', 'issuedAt', 'credentialExpiresAt', 'observedAt'].includes(key) && typeof object[key] === 'string'
      ? canonicalRFC3339Nano(object[key]) : object[key];
    return `${encodeCanonicalValue(key)}:${encodeCanonicalValue(fieldValue, childContext)}`;
  }).join(',')}}`;
}

function canonicalRFC3339Nano(value: string): string {
  const parts = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!parts || Number.isNaN(Date.parse(value))) throw new TypeError('project timestamp must be RFC3339Nano with at most nine fractional digits');
  const fraction = (parts[2] ?? '').replace(/0+$/, '');
  const offset = parts[3] === '+00:00' || parts[3] === '-00:00' ? 'Z' : parts[3];
  return `${parts[1]}${fraction ? `.${fraction}` : ''}${offset}`;
}

function shouldOmitGoField(object: Record<string, unknown>, key: string): boolean {
  const value = object[key];
  if (value === undefined) return true;
  if (['expiresAt', 'credential', 'mutation', 'credentialBroker', 'secretRef'].includes(key)) return value === null;
  if (['authMethodId', 'sharedConnectionId', 'attemptId'].includes(key)) return value === '';
  if (key === 'environment') return value === null || (typeof value === 'object' && !Array.isArray(value) && Object.keys(value).length === 0);
  return false;
}

function canonicalFieldOrder(object: Record<string, unknown>): string[] {
  const keys = Object.keys(object);
  let preferred: string[] = [];
  if ('schemaVersion' in object && 'operationConfigurations' in object) preferred = ['schemaVersion', 'scope', 'revision', 'connections', 'triggerBindings', 'operationConfigurations', 'environment', 'credentialBroker'];
  else if ('schemaVersion' in object && 'status' in object) preferred = ['schemaVersion', 'connection', 'scope', 'revision', 'fence', 'status', 'expiresAt', 'authMethod', 'credential', 'mutation'];
  else if ('schemaVersion' in object && 'credentials' in object) preferred = ['schemaVersion', 'scope', 'connection', 'fence', 'attemptId', 'credentials', 'expiresAt', 'authMethod'];
  else if ('projectId' in object && 'kind' in object) preferred = ['projectId', 'kind'];
  else if ('connectorId' in object && 'modulePath' in object) preferred = ['connectorId', 'connectionName', 'modulePath', 'provider', 'authMethodId', 'sharedConnectionId', 'configuration'];
  else if ('connectorId' in object && 'triggerName' in object) preferred = ['connectorId', 'connectionName', 'triggerName', 'bindingName', 'configuration'];
  else if ('connectorId' in object && 'flowType' in object) preferred = ['connectorId', 'connectionName', 'operationId', 'flowType', 'stepType', 'configuration'];
  else if ('connectorId' in object && 'connectionName' in object) preferred = ['connectorId', 'connectionName'];
  else if ('kind' in object && 'invocationId' in object) preferred = ['kind', 'invocationId', 'id', 'fence', 'baseRevision', 'deadline', 'resultKey'];
  else if ('endpoint' in object && 'tokenSecretRef' in object) preferred = ['endpoint', 'tokenSecretRef'];
  else if ('value' in object || 'secretRef' in object) preferred = ['value', 'secretRef'];
  else if ('key' in object && 'version' in object && 'digest' in object) preferred = ['key', 'version', 'digest'];
  const selected = preferred.filter(key => keys.includes(key));
  return selected.concat(keys.filter(key => !selected.includes(key)).sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right))));
}

function validateContents(contents: Uint8Array): void {
  if (!contents.length || contents.length > 4 << 20) throw new TypeError('project object body is empty or oversized');
}

function classifyStorageError(error: unknown, isWrite: boolean): Error {
  const code = error !== null && typeof error === 'object' && 'name' in error ? String(error.name) : '';
  if (['NoSuchKey', 'NoSuchVersion', 'NotFound'].includes(code)) return new ObjectNotFoundError();
  if (['PreconditionFailed', 'ConditionalRequestConflict', 'OperationAborted'].includes(code)) return new ConflictError();
  return isWrite ? new OutcomeUnknownError() : new Error('project object read failed');
}
