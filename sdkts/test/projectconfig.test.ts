// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, writeFile, chmod, rm, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Readable } from 'node:stream';
import { inspect } from 'node:util';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { GetBucketVersioningCommand, GetObjectCommand, PutObjectCommand, S3Client } from '@aws-sdk/client-s3';
import { ReauthorizationRequiredError } from '../src/errors.js';
import { CredentialBrokerClient, CredentialBrokerRejectedError, CredentialBrokerUnavailableError } from '../src/projectconfig/broker.js';
import { LoadedProject, loadFromEnvironment, readLocalConnectionsFile } from '../src/projectconfig/environment.js';
import { scopePrefix, ConfigurationSchema } from '../src/projectconfig/model.js';
import { ConfigurationStore, ConflictError, ObjectNotFoundError, OutcomeUnknownError, ProjectObject, S3ObjectStore, digestBytes, encodeGoCompatibleJSON, validateObjectKey, type ObjectStore } from '../src/projectconfig/storage.js';

const paths: string[] = [];
afterEach(async () => { await Promise.all(paths.splice(0).map(path => rm(path, { recursive: true, force: true }))); });

describe('project configuration', () => {
  it('reads a private Dex Web file and resolves current credentials', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'sdkts-project-'));
    paths.push(directory);
    const path = join(directory, 'connections.json');
    const file = { schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1', connections: [{ connectorId: 'newsapi', connectionName: 'default', modulePath: 'github.com/example/newsapi', provider: 'newsapi', configuration: {}, credentials: { apiKey: 'private' } }] };
    await writeFile(path, JSON.stringify(file), { mode: 0o600 });
    const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
    expect(project.configuration.connections[0]?.connectionName).toBe('default');
    expect(await project.resolveCredential('newsapi', 'default')).toEqual({ apiKey: 'private' });
    await chmod(path, 0o644);
    await expect(readLocalConnectionsFile(path)).rejects.toThrow('not valid JSON or not private');
  });

  it('validates scope, secret-safe config, and snapshot digest', async () => {
    const scope = { projectId: 'p1', kind: 'preview' as const };
    expect(scopePrefix(scope)).toBe('projects/p1/preview');
    expect(() => validateObjectKey('../secret')).toThrow();
    const configuration = ConfigurationSchema.parse({ schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, connections: [], triggerBindings: [], operationConfigurations: [] });
    const contents = Buffer.from(JSON.stringify(configuration));
    const store: ObjectStore = {
      async readObject(key) { return new ProjectObject(key, 'v1', 'etag', contents); },
      async createObject() { throw new Error('unused'); },
      async compareAndSwapObject() { throw new Error('unused'); },
    };
    const configs = new ConfigurationStore(store, scope);
    const snapshot = await configs.freezeConfiguration(1);
    expect(snapshot.digest).toBe(digestBytes(contents));
    expect(await configs.readSnapshot(snapshot)).toEqual(configuration);
    expect(() => JSON.stringify(new ProjectObject('key', 'v1', 'etag', contents))).toThrow('cannot be serialized');
    const privateObject = new ProjectObject('key', 'v1', 'etag', Buffer.from('private-token'));
    expect(inspect(privateObject)).toBe('ProjectObject{key version=v1}');
    expect(inspect({ object: privateObject })).toBe('{ object: ProjectObject{key version=v1} }');
    expect(inspect({ object: privateObject })).not.toContain('70 72 69');
    expect(encodeGoCompatibleJSON({ schemaVersion: 'v', scope, revision: 1, connections: [], triggerBindings: [], operationConfigurations: [], environment: {} })).not.toContain('environment');
    expect(encodeGoCompatibleJSON({ configuration: { z: 'x<>&\u2028\u2029', a: 1 } })).toBe('{"configuration":{"a":1,"z":"x\\u003c\\u003e\\u0026\\u2028\\u2029"}}');
  });

  it('writes configuration through revision and ETag CAS', async () => {
    const scope = { projectId: 'p2', kind: 'live' as const };
    let object: ProjectObject | undefined;
    const store: ObjectStore = {
      async readObject() { if (!object) throw new ObjectNotFoundError(); return object; },
      async createObject(key, contents) { if (object) throw new ConflictError(); object = new ProjectObject(key, 'v1', 'etag-1', contents); return object; },
      async compareAndSwapObject(key, expected, contents) { if (object?.etag !== expected) throw new ConflictError(); object = new ProjectObject(key, 'v2', 'etag-2', contents); return object; },
    };
    const configs = new ConfigurationStore(store, scope);
    const document = { connections: [], triggerBindings: [], operationConfigurations: [] };
    expect((await configs.writeConfiguration(0, document)).revision).toBe(1);
    await expect(configs.writeConfiguration(0, document)).rejects.toBeInstanceOf(ConflictError);
    expect((await configs.writeConfiguration(1, document)).revision).toBe(2);
  });

  it('validates broker answers and classifies transport failures', async () => {
    const broker = new CredentialBrokerClient('https://broker.example.test/read', async () => 'scope-token', async (_input, init) => {
      expect(init?.redirect).toBe('manual');
      expect(init?.body).toBe('{"sharedConnectionId":"conn-7","rejectedRevision":3}');
      return new Response(JSON.stringify({ status: 'READY', credential: { accessToken: 'secret' }, revision: 4 }), { status: 200 });
    });
    expect(await broker.read('conn-7', 3)).toMatchObject({ revision: 4 });
    const unavailable = new CredentialBrokerClient('https://broker.example.test/read', async () => 'scope-token', async () => new Response('', { status: 503 }));
    await expect(unavailable.read('conn-7')).rejects.toBeInstanceOf(CredentialBrokerUnavailableError);
    const rejected = new CredentialBrokerClient('https://broker.example.test/read', async () => 'scope-token', async () => new Response('bad', { status: 200 }));
    await expect(rejected.read('conn-7')).rejects.toBeInstanceOf(CredentialBrokerRejectedError);
    let calls = 0;
    const rotated = new CredentialBrokerClient('https://broker.example.test/read', async renew => renew ? 'new-token' : 'old-token', async (_input, init) => {
      calls++;
      return init?.headers && (init.headers as Record<string, string>).Authorization === 'Bearer old-token'
        ? new Response('', { status: 401 })
        : new Response(JSON.stringify({ status: 'READY', credential: { accessToken: 'secret' }, revision: 5 }), { status: 200 });
    });
    expect((await rotated.read('conn-7')).revision).toBe(5);
    expect(calls).toBe(2);
    let tokenReads = 0;
    const cachedToken = new CredentialBrokerClient('https://broker.example.test/read', async () => { tokenReads++; return 'scope-token'; }, async () => new Response(JSON.stringify({ status: 'READY', credential: { accessToken: 'private' }, revision: 1 }), { status: 200 }));
    await cachedToken.read('conn-7');
    await cachedToken.read('conn-7');
    expect(tokenReads).toBe(1);
    const brokenBody = new CredentialBrokerClient('https://broker.example.test/read', async () => 'scope-token', async () => new Response(new ReadableStream({ start(controller) { controller.error(new Error('network body failure')); } }), { status: 200 }));
    await expect(brokenBody.read('conn-7')).rejects.toBeInstanceOf(CredentialBrokerUnavailableError);
    const oversizedBody = new CredentialBrokerClient('https://broker.example.test/read', async () => 'scope-token', async () => new Response('x'.repeat((1 << 20) + 1), { status: 200 }));
    await expect(oversizedBody.read('conn-7')).rejects.toBeInstanceOf(CredentialBrokerRejectedError);
    const controller = new AbortController();
    const pending = new CredentialBrokerClient('https://broker.example.test/read', async () => 'scope-token', async (_input, init) => new Promise<Response>((_resolve, reject) => { init?.signal?.addEventListener('abort', () => reject(new Error('aborted')), { once: true }); }));
    const pendingRead = pending.read('conn-7', 0, controller.signal);
    controller.abort();
    await expect(pendingRead).rejects.toBeInstanceOf(CredentialBrokerUnavailableError);
  });

  it('reuses a fresh shared credential and obtains a new rejected revision', async () => {
    let brokerReads = 0;
    const broker = new CredentialBrokerClient('https://broker.example.test/read', async () => 'scope-token', async () => {
      brokerReads++;
      return new Response(JSON.stringify({ status: 'READY', credential: { accessToken: `token-${brokerReads}` }, revision: brokerReads, expiresAt: new Date(Date.now() + 3600000).toISOString() }), { status: 200 });
    });
    const scope = { projectId: 'p3', kind: 'live' as const };
    const project = new LoadedProject({ schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha3', scope, revision: 1,
      connections: [{ connectorId: 'sample', connectionName: 'shared', modulePath: 'sample', provider: 'sample', sharedConnectionId: 'conn-7', configuration: {} }],
      triggerBindings: [], operationConfigurations: [], credentialBroker: {
        endpoint: 'https://broker.example.test/read', tokenSecretRef: {
          key: `projects/p3/live/app-secrets/DEX_CREDENTIAL_BROKER_TOKEN/${'a'.repeat(32)}`,
          version: 'v1', digest: `sha256:${'b'.repeat(64)}`,
        },
      },
    }, undefined, undefined, undefined, broker);
    expect(await project.resolveCredential('sample', 'shared')).toEqual({ accessToken: 'token-1' });
    expect(await project.resolveCredential('sample', 'shared')).toEqual({ accessToken: 'token-1' });
    expect(await project.resolveCredentialAfterRejection('sample', 'shared', 1)).toEqual({ accessToken: 'token-2' });
    expect(brokerReads).toBe(2);
  });

  it('treats terminal and expired private credentials as unavailable without leaking source text', async () => {
    const fixture = JSON.parse(await readFile(new URL('../../conformance/project-storage.json', import.meta.url), 'utf8')) as {
      scope: { projectId: string; kind: 'preview' }; completedConnectionRecord: Record<string, unknown>;
      credentialEnvelope: Record<string, unknown>; connectionHeadKey: string; credentialObjectKey: string;
    };
    const expiresAt = '2035-01-01T12:00:00+08:00';
    const record = { ...fixture.completedConnectionRecord, expiresAt, credential: { key: fixture.credentialObjectKey, version: '1', digest: '' } };
    const envelope = { ...fixture.credentialEnvelope, fence: 2, expiresAt };
    const privateBytes = Buffer.from(JSON.stringify(envelope));
    record.credential.digest = digestBytes(privateBytes).slice(7);
    let recordBytes = Buffer.from(JSON.stringify(record));
    let envelopeBytes = privateBytes;
    const objects: ObjectStore = {
      async readObject(key) { return new ProjectObject(key, '1', 'etag', key === fixture.connectionHeadKey ? recordBytes : envelopeBytes); },
      async createObject() { throw new Error('unused'); }, async compareAndSwapObject() { throw new Error('unused'); },
    };
    const project = new LoadedProject({ schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope: fixture.scope, revision: 1,
      connections: [{ connectorId: 'sample', connectionName: 'main-é-流程 😀', modulePath: 'sample', provider: 'sample', configuration: {} }], triggerBindings: [], operationConfigurations: [],
    }, undefined, objects, undefined, undefined);
    expect(await project.resolveCredential('sample', 'main-é-流程 😀')).toEqual({ accessToken: 'REDACTED_ACCESS_TOKEN' });
    for (const status of ['REAUTHORIZATION_REQUIRED', 'REVOKED']) {
      recordBytes = Buffer.from(JSON.stringify({ ...record, status }));
      await expect(project.resolveCredential('sample', 'main-é-流程 😀')).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    }
    recordBytes = Buffer.from(JSON.stringify({ ...record, expiresAt: '2020-01-01T00:00:00Z' }));
    await expect(project.resolveCredential('sample', 'main-é-流程 😀')).rejects.toThrow('expired project credential');
    recordBytes = Buffer.from(JSON.stringify(record));
    envelopeBytes = Buffer.from(JSON.stringify({ ...envelope, expiresAt: '2035-01-01T12:00:00.000000001+08:00' }));
    recordBytes = Buffer.from(JSON.stringify({ ...record, expiresAt: '2035-01-01T12:00:00.000000002+08:00', credential: { ...record.credential, digest: digestBytes(envelopeBytes).slice(7) } }));
    await expect(project.resolveCredential('sample', 'main-é-流程 😀')).rejects.toThrow('identity differs');
    recordBytes = Buffer.from('sk-live-record-secret');
    await expect(project.resolveCredential('sample', 'main-é-流程 😀')).rejects.toThrow('project credential is not valid JSON');
    recordBytes = Buffer.from(JSON.stringify(record));
    envelopeBytes = Buffer.from('sk-live-envelope-secret');
    try { await project.resolveCredential('sample', 'main-é-流程 😀'); throw new Error('expected invalid credential'); }
    catch (error: unknown) { expect(String(error)).not.toContain('sk-live'); }
  });

  it('checks local credential expiry and hosted environment combinations', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'sdkts-expiry-'));
    paths.push(directory);
    const path = join(directory, 'connections.json');
    await writeFile(path, JSON.stringify({ schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1', connections: [{ connectorId: 'sample', connectionName: 'main', modulePath: 'sample', provider: 'sample', configuration: {}, credentials: { token: 'private' }, credentialExpiresAt: '2020-01-01T00:00:00Z' }] }), { mode: 0o600 });
    const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
    await expect(project.resolveCredential('sample', 'main')).rejects.toThrow('expired project credential');
    await expect(loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path, DEX_PROJECT_ID: 'other' })).rejects.toThrow('different project configurations');
    await expect(loadFromEnvironment({ DEX_PROJECT_ID: 'p1', DEX_PROJECT_SCOPE: 'preview', DEX_PROJECT_STORAGE_BUCKET: 'test', DEX_PROJECT_STORAGE_ENDPOINT: 'http://127.evil.com:9000', DEX_PROJECT_ALLOW_LOCAL_STORAGE: 'true' })).rejects.toThrow('local fixture boundary');
    await expect(loadFromEnvironment({ DEX_PROJECT_ID: 'p1', DEX_PROJECT_SCOPE: 'preview', DEX_PROJECT_STORAGE_BUCKET: 'test', DEX_PROJECT_STORAGE_ENDPOINT: 'http://localhost:9000' })).rejects.toThrow('opt-in');
  });

  it('passes S3 CAS and KMS parameters and classifies storage errors', async () => {
    const client = new S3Client({ region: 'us-east-1', maxAttempts: 1 });
    const kmsKeyId = 'arn:aws:kms:us-east-1:123456789012:key/abc123';
    const commands: unknown[] = [];
    let writeFailureName = '';
    let readFailureName = '';
    const send = vi.spyOn(client, 'send').mockImplementation(async command => {
      commands.push(command);
      if (command instanceof GetBucketVersioningCommand) return { Status: 'Enabled' } as never;
      if (command instanceof GetObjectCommand) {
        if (readFailureName) throw Object.assign(new Error('secret-containing AWS error'), { name: readFailureName });
        return { VersionId: '1', ETag: 'etag', Body: Readable.from([Buffer.from('{}')]), ServerSideEncryption: 'aws:kms', SSEKMSKeyId: kmsKeyId } as never;
      }
      if (command instanceof PutObjectCommand) {
        if (writeFailureName) throw Object.assign(new Error('secret-containing AWS error'), { name: writeFailureName });
        return { VersionId: '2', ETag: 'new-etag' } as never;
      }
      throw new Error('unexpected command');
    });
    const store = await S3ObjectStore.create({ client, bucket: 'private-bucket', kmsKeyId, prefix: 'prefix' });
    await store.createObject('project/head', Buffer.from('{}'));
    await store.compareAndSwapObject('project/head', 'etag', Buffer.from('{}'));
    expect((commands[1] as PutObjectCommand).input).toMatchObject({ Key: 'prefix/project/head', IfNoneMatch: '*', ServerSideEncryption: 'aws:kms', SSEKMSKeyId: kmsKeyId });
    expect((commands[2] as PutObjectCommand).input).toMatchObject({ Key: 'prefix/project/head', IfMatch: 'etag', ServerSideEncryption: 'aws:kms', SSEKMSKeyId: kmsKeyId });
    expect(Buffer.from((await store.readObject('project/head', '1')).contents).toString()).toBe('{}');
    for (const name of ['PreconditionFailed', 'ConditionalRequestConflict', 'OperationAborted']) {
      writeFailureName = name;
      await expect(store.createObject('project/head', Buffer.from('{}'))).rejects.toBeInstanceOf(ConflictError);
    }
    writeFailureName = 'TimeoutError';
    await expect(store.createObject('project/head', Buffer.from('{}'))).rejects.toBeInstanceOf(OutcomeUnknownError);
    for (const name of ['NoSuchKey', 'NoSuchVersion', 'NotFound']) {
      readFailureName = name;
      await expect(store.readObject('project/head')).rejects.toBeInstanceOf(ObjectNotFoundError);
    }
    readFailureName = 'TimeoutError';
    await expect(store.readObject('project/head')).rejects.toThrow('project object read failed');
    send.mockRestore();
  });

  it('rejects a hosted snapshot with a mismatched digest', async () => {
    const scope = { projectId: 'hosted', kind: 'preview' as const };
    const configuration = { schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, connections: [], triggerBindings: [], operationConfigurations: [] };
    const kmsKeyId = 'arn:aws:kms:us-east-1:123456789012:key/abc123';
    const send = vi.spyOn(S3Client.prototype, 'send').mockImplementation(async command => {
      if (command instanceof GetBucketVersioningCommand) return { Status: 'Enabled' } as never;
      if (command instanceof GetObjectCommand) return { VersionId: '1', ETag: 'etag', Body: Readable.from([Buffer.from(JSON.stringify(configuration))]), ServerSideEncryption: 'aws:kms', SSEKMSKeyId: kmsKeyId } as never;
      throw new Error('unexpected command');
    });
    try {
      await expect(loadFromEnvironment({ DEX_PROJECT_ID: scope.projectId, DEX_PROJECT_SCOPE: scope.kind, DEX_PROJECT_STORAGE_BUCKET: 'private-bucket', DEX_PROJECT_STORAGE_KMS_KEY_ARN: kmsKeyId,
        DEX_PROJECT_CONFIG_KEY: `${scopePrefix(scope)}/configuration/head`, DEX_PROJECT_CONFIG_VERSION: '1', DEX_PROJECT_CONFIG_DIGEST: `sha256:${'0'.repeat(64)}` })).rejects.toThrow('digest differs');
    } finally { send.mockRestore(); }
  });
});
