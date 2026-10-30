// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFile, lstat } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { S3Client } from '@aws-sdk/client-s3';
import { z } from 'zod';
import { ConfigurationSchema, LocalConnectionsFileSchema, RFC3339NanoSchema, type Configuration, type LocalConnection, type Scope, type SnapshotRef, scopePrefix } from './model.js';
import { ConfigurationStore, S3ObjectStore, digestBytes, type ObjectStore } from './storage.js';
import { CredentialBrokerClient, CredentialBrokerUnavailableError, RejectedCredentialNotRefreshedError, type BrokerCredential } from './broker.js';
import { isLocalStorageHost } from '../netpolicy.js';
import { ReauthorizationRequiredError } from '../errors.js';

/** Selects the private local connections.json written by Dex Web. */
export const LocalConnectionsFileEnvironment = 'DEX_CONNECTOR_CONFIG_FILE';

/** Loaded ordinary configuration and private connection resolver. */
export class LoadedProject {
  readonly #sharedCredentials = new Map<string, { result: BrokerCredential; readAt: number; notBefore: number }>();
  /** Bind startup-pinned configuration to exactly one private credential source. */
  constructor(
    /** Startup-pinned ordinary document. */ readonly configuration: Configuration,
    /** Exact immutable document reference. */ readonly snapshot: SnapshotRef | undefined,
    private readonly objects: ObjectStore | undefined,
    private readonly localFilePath: string | undefined,
    private readonly broker: CredentialBrokerClient | undefined,
  ) { ConfigurationSchema.parse(configuration); }

  /** Read a current private credential, never returning it through a Flow result. */
  async resolveCredential(connectorId: string, connectionName: string, signal?: AbortSignal): Promise<Record<string, unknown>> {
    const connection = this.configuration.connections.find(entry => entry.connectorId === connectorId && entry.connectionName === connectionName);
    if (!connection) throw new Error('project connection is not configured');
    if (connection.sharedConnectionId) {
      if (!this.broker) throw new Error('credential broker is not configured');
      const cached = this.#sharedCredentials.get(connection.sharedConnectionId);
      if (cached && isSharedCredentialFresh(cached, Date.now())) return cached.result.credential;
      try {
        const result = await this.broker.read(connection.sharedConnectionId, 0, signal);
        this.#sharedCredentials.set(connection.sharedConnectionId, { result, readAt: Date.now(), notBefore: Date.now() + 1000 });
        return result.credential;
      } catch (error: unknown) {
        if (error instanceof CredentialBrokerUnavailableError && cached && isSharedCredentialUnexpired(cached.result, Date.now())) {
          cached.notBefore = Date.now() + 10000;
          return cached.result.credential;
        }
        throw error;
      }
    }
    if (this.localFilePath) {
      const localFile = await readLocalConnectionsFile(this.localFilePath);
      const selected = localFile.connections.find(entry => entry.connectorId === connectorId && entry.connectionName === connectionName);
      if (!selected) throw new Error('project connection is not configured');
      if (isExpired(selected.credentialExpiresAt)) throw new Error('expired project credential requires refresh');
      return selected.credentials;
    }
    if (!this.objects) throw new Error('project object store is unavailable');
    const scope = this.configuration.scope;
    const connectionPath = `${scopePrefix(scope)}/connections/${connectorId}/${Buffer.from(connectionName).toString('base64url')}`;
    const head = await this.objects.readObject(`${connectionPath}/head`);
    const record = ConnectionRecordSchema.safeParse(parsePrivateJSON(head.contents));
    if (!record.success) throw new Error('project credential record violates its schema');
    if (record.data.status === 'REAUTHORIZATION_REQUIRED' || record.data.status === 'REVOKED') throw new ReauthorizationRequiredError();
    if (record.data.connection.connectorId !== connectorId || record.data.connection.connectionName !== connectionName || record.data.scope.projectId !== scope.projectId || record.data.scope.kind !== scope.kind || record.data.status !== 'READY' || !record.data.credential?.key.startsWith(`${connectionPath}/`)) throw new Error('project credential is unavailable');
    if (isExpired(record.data.expiresAt)) throw new Error('expired project credential requires refresh');
    const privateObject = await this.objects.readObject(record.data.credential.key, record.data.credential.version);
    const envelope = CredentialEnvelopeSchema.safeParse(parsePrivateJSON(privateObject.contents));
    if (!envelope.success) throw new Error('project credential envelope violates its schema');
    if (digestBytes(privateObject.contents).slice(7) !== record.data.credential.digest || envelope.data.scope.projectId !== scope.projectId || envelope.data.scope.kind !== scope.kind || envelope.data.connection.connectorId !== connectorId || envelope.data.connection.connectionName !== connectionName || envelope.data.fence !== record.data.fence || envelope.data.authMethod !== record.data.authMethod || !sameExpiry(envelope.data.expiresAt, record.data.expiresAt)) throw new Error('project credential identity differs');
    if (isExpired(envelope.data.expiresAt)) throw new Error('expired project credential requires refresh');
    return envelope.data.credentials;
  }

  /** Resolve a newer shared credential after the provider rejected one revision. */
  async resolveCredentialAfterRejection(connectorId: string, connectionName: string, rejectedRevision: number, signal?: AbortSignal): Promise<Record<string, unknown>> {
    const connection = this.configuration.connections.find(entry => entry.connectorId === connectorId && entry.connectionName === connectionName);
    if (!connection?.sharedConnectionId || !this.broker || !Number.isSafeInteger(rejectedRevision) || rejectedRevision < 1) throw new TypeError('shared credential rejection identity is invalid');
    const cached = this.#sharedCredentials.get(connection.sharedConnectionId);
    if (cached && cached.result.revision > rejectedRevision && isSharedCredentialUnexpired(cached.result, Date.now())) return cached.result.credential;
    const result = await this.broker.read(connection.sharedConnectionId, rejectedRevision, signal);
    if (result.revision === rejectedRevision) throw new RejectedCredentialNotRefreshedError();
    this.#sharedCredentials.set(connection.sharedConnectionId, { result, readAt: Date.now(), notBefore: Date.now() + 1000 });
    return result.credential;
  }
}

function isSharedCredentialUnexpired(result: BrokerCredential, now: number): boolean {
  return !result.expiresAt || Date.parse(result.expiresAt) > now;
}

function isSharedCredentialFresh(cached: { result: BrokerCredential; readAt: number; notBefore: number }, now: number): boolean {
  if (!isSharedCredentialUnexpired(cached.result, now)) return false;
  if (now < cached.notBefore) return true;
  if (!cached.result.expiresAt) return now - cached.readAt < 15 * 60 * 1000;
  const lifetime = cached.result.issuedAt ? Date.parse(cached.result.expiresAt) - Date.parse(cached.result.issuedAt) : 0;
  const skew = lifetime > 0 ? Math.min(5 * 60 * 1000, lifetime / 4) : 5 * 60 * 1000;
  return Date.parse(cached.result.expiresAt) > now + skew;
}

/** Strict private connection record compatible with sdkgo. */
export const ConnectionRecordSchema = z.strictObject({
  schemaVersion: z.literal('connectors.dex.dev/project-connection/v1alpha2'), connection: z.strictObject({ connectorId: z.string(), connectionName: z.string() }),
  scope: z.strictObject({ projectId: z.string(), kind: z.string() }), revision: z.number().int().positive(),
  fence: z.number().int().positive(), status: z.enum(['READY', 'AUTHORIZING', 'REFRESHING', 'REAUTHORIZATION_REQUIRED', 'REVOKED']), authMethod: z.string(), expiresAt: RFC3339NanoSchema.nullable().optional(),
  credential: z.strictObject({ key: z.string(), version: z.string(), digest: z.string() }).optional(),
  mutation: z.strictObject({ kind: z.string(), invocationId: z.string(), id: z.string(), fence: z.number(), baseRevision: z.number(), deadline: RFC3339NanoSchema, resultKey: z.string() }).optional(),
});
/** Strict private credential envelope compatible with sdkgo. */
export const CredentialEnvelopeSchema = z.strictObject({
  schemaVersion: z.literal('connectors.dex.dev/project-connection/v1alpha2'),
  scope: z.strictObject({ projectId: z.string(), kind: z.string() }), connection: z.strictObject({ connectorId: z.string(), connectionName: z.string() }),
  fence: z.number().int().positive(), attemptId: z.string().optional(), credentials: z.record(z.string(), z.unknown()),
  expiresAt: RFC3339NanoSchema.nullable().optional(), authMethod: z.string(),
});

function parsePrivateJSON(contents: Uint8Array): unknown {
  try { return JSON.parse(Buffer.from(contents).toString('utf8')) as unknown; }
  catch { throw new Error('project credential is not valid JSON'); }
}

function isExpired(expiresAt: string | null | undefined): boolean { return Boolean(expiresAt && Date.parse(expiresAt) <= Date.now()); }

function sameExpiry(left: string | null | undefined, right: string | null | undefined): boolean {
  return left && right ? rfc3339Nanoseconds(left) === rfc3339Nanoseconds(right) : !left && !right;
}

function rfc3339Nanoseconds(value: string): bigint {
  const parts = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!parts) throw new Error('project credential expiry is invalid');
  const milliseconds = Date.parse(`${parts[1]}${parts[3]}`);
  if (!Number.isFinite(milliseconds)) throw new Error('project credential expiry is invalid');
  return BigInt(milliseconds) * 1_000_000n + BigInt((parts[2] ?? '').padEnd(9, '0') || '0');
}

/** Load trusted DEX_PROJECT_* settings or a local Dex Web file at startup. */
export async function loadFromEnvironment(environment: NodeJS.ProcessEnv = process.env): Promise<LoadedProject> {
  const localFilePath = environment[LocalConnectionsFileEnvironment];
  if (localFilePath) {
    for (const name of ['DEX_PROJECT_ID', 'DEX_PROJECT_SCOPE', 'DEX_PROJECT_CONFIG_KEY', 'DEX_PROJECT_STORAGE_BUCKET']) {
      if (environment[name]) throw new Error(`${LocalConnectionsFileEnvironment} and ${name} select different project configurations`);
    }
    const file = await readLocalConnectionsFile(localFilePath);
    const scope: Scope = { projectId: 'local', kind: 'preview' };
    const useFilePath = join(dirname(localFilePath), 'use-configurations.json');
    let operationConfigurations: Configuration['operationConfigurations'] = [];
    try {
      const useFile = z.object({ schemaVersion: z.literal('connectors.dex.dev/local-use-configurations/v1alpha1'), operationConfigurations: z.array(z.object({ connectorId: z.string(), connectionName: z.string(), operationId: z.string(), flowType: z.string(), stepType: z.string(), configuration: z.record(z.string(), z.unknown()) })) }).parse(JSON.parse(await readPrivateFile(useFilePath)) as unknown);
      operationConfigurations = useFile.operationConfigurations;
    } catch (error: unknown) {
      if (!(error !== null && typeof error === 'object' && 'code' in error && error.code === 'ENOENT')) throw new Error('connector use configuration file is invalid');
    }
    const configuration: Configuration = {
      schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1,
      connections: file.connections.map(localConnectionConfiguration), triggerBindings: file.triggerBindings ?? [], operationConfigurations,
    };
    return new LoadedProject(configuration, undefined, undefined, localFilePath, undefined);
  }
  const scope = { projectId: environment.DEX_PROJECT_ID ?? '', kind: environment.DEX_PROJECT_SCOPE ?? '' } as Scope;
  scopePrefix(scope);
  const allowLocalStorage = environment.DEX_PROJECT_ALLOW_LOCAL_STORAGE === 'true';
  if (!['', 'true', 'false'].includes(environment.DEX_PROJECT_ALLOW_LOCAL_STORAGE ?? '')) throw new Error('project local storage opt-in must be true or false');
  const endpoint = environment.DEX_PROJECT_STORAGE_ENDPOINT;
  if (Boolean(endpoint) !== allowLocalStorage) throw new Error('local project storage requires explicit local endpoint and opt-in');
  if (endpoint && !isLocalStorageEndpoint(endpoint)) throw new Error('custom project storage endpoint requires a local fixture boundary');
  const bucket = environment.DEX_PROJECT_STORAGE_BUCKET;
  if (!bucket) throw new Error('project storage bucket is required');
  const client = new S3Client(endpoint ? { endpoint, forcePathStyle: true, maxAttempts: 1 } : { maxAttempts: 1 });
  const objects = await S3ObjectStore.create({ client, bucket, allowUnencrypted: allowLocalStorage,
    ...(environment.DEX_PROJECT_STORAGE_PREFIX ? { prefix: environment.DEX_PROJECT_STORAGE_PREFIX } : {}),
    ...(environment.DEX_PROJECT_STORAGE_KMS_KEY_ARN ? { kmsKeyId: environment.DEX_PROJECT_STORAGE_KMS_KEY_ARN } : {}),
  });
  const snapshot: SnapshotRef = {
    key: environment.DEX_PROJECT_CONFIG_KEY ?? '', version: environment.DEX_PROJECT_CONFIG_VERSION ?? '',
    digest: environment.DEX_PROJECT_CONFIG_DIGEST ?? '', mediaType: 'application/json',
  };
  const configuration = await new ConfigurationStore(objects, scope).readSnapshot(snapshot);
  let tokenReference = configuration.credentialBroker?.tokenSecretRef;
  const broker = configuration.credentialBroker ? new CredentialBrokerClient(configuration.credentialBroker.endpoint, async (renew = false) => {
    if (renew) tokenReference = (await new ConfigurationStore(objects, scope).readConfiguration()).configuration.credentialBroker?.tokenSecretRef;
    if (!tokenReference) throw new Error('credential broker token reference is unavailable');
    const tokenObject = await objects.readObject(tokenReference.key, tokenReference.version);
    if (digestBytes(tokenObject.contents) !== tokenReference.digest) throw new Error('credential broker token integrity differs');
    return Buffer.from(tokenObject.contents).toString('utf8');
  }) : undefined;
  return new LoadedProject(configuration, snapshot, objects, undefined, broker);
}

/** Read a mode-0600 regular file without exposing private values in errors. */
export async function readLocalConnectionsFile(path: string): Promise<{ connections: LocalConnection[]; triggerBindings?: Configuration['triggerBindings'] }> {
  let parsed: unknown;
  try { parsed = JSON.parse(await readPrivateFile(path)) as unknown; }
  catch { throw new Error('connector configuration file is not valid JSON or not private'); }
  const result = LocalConnectionsFileSchema.safeParse(parsed);
  if (!result.success) throw new Error('connector configuration file violates its schema');
  return { connections: result.data.connections, ...(result.data.triggerBindings ? { triggerBindings: result.data.triggerBindings } : {}) };
}

async function readPrivateFile(path: string): Promise<string> {
  const info = await lstat(path);
  if (!info.isFile() || info.isSymbolicLink() || (info.mode & 0o777) !== 0o600) throw new Error('connector configuration file must be a regular mode-0600 file');
  return readFile(path, 'utf8');
}

function localConnectionConfiguration(entry: LocalConnection): Configuration['connections'][number] {
  return { connectorId: entry.connectorId, connectionName: entry.connectionName, modulePath: entry.modulePath, provider: entry.provider, configuration: entry.configuration,
    ...(entry.authMethodId ? { authMethodId: entry.authMethodId } : {}) };
}

function isLocalStorageEndpoint(value: string): boolean {
  try {
    const endpoint = new URL(value);
    const hostname = endpoint.hostname;
    return ['http:', 'https:'].includes(endpoint.protocol) && !endpoint.username && !endpoint.password && !endpoint.search && !endpoint.hash &&
      (!endpoint.pathname || endpoint.pathname === '/') && isLocalStorageHost(hostname);
  } catch { return false; }
}
