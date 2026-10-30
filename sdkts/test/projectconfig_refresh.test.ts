// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, readFile, stat, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import type { AsyncContext } from '@superdurable/dex';
import { describe, expect, it } from 'vitest';
import { createCall, SecretString, type CredentialRefreshDriver } from '../src/credentials.js';
import { ReauthorizationRequiredError } from '../src/errors.js';
import { TokenEndpoint } from '../src/oauthtoken/index.js';
import { ConnectionStore, CredentialMaterial, connectionPath, connectionRecord, credentialEnvelope, type ConnectionKey } from '../src/projectconfig/connections.js';
import { loadFromEnvironment } from '../src/projectconfig/environment.js';
import { CredentialProvider, RefreshFailedError, RefreshingCredentialProvider, RejectedCredentialNotRefreshedError } from '../src/projectconfig/index.js';
import { InMemoryObjectStore, OutcomeUnknownError, encodeGoCompatibleJSON, type ObjectStore } from '../src/projectconfig/storage.js';

const scope = { projectId: 'test', kind: 'preview' } as const;
const key: ConnectionKey = { connectorId: 'sample', connectionName: 'default' };
const context = { flowId: 'flow-1', stepExecutionId: 'refresh:1', attempt: 1, cancellationSignal: new AbortController().signal } as AsyncContext;
const call = createCall(context, { provider: 'sample', name: 'default' }, { connectorId: 'sample', operationId: 'getItem' });
const decode = (document: Record<string, unknown>) => {
  if (typeof document.accessToken !== 'string' || typeof document.refreshToken !== 'string') throw new TypeError();
  return { accessToken: document.accessToken, refreshToken: document.refreshToken };
};
const encode = (credentials: ReturnType<typeof decode>) => ({ ...credentials });
const future = () => new Date(Date.now() + 3_600_000);
const expired = () => new Date(Date.now() - 1_000);
const driver = (refresh: CredentialRefreshDriver<ReturnType<typeof decode>>['refresh']): CredentialRefreshDriver<ReturnType<typeof decode>> => ({ refreshRequired: () => true, refresh });

async function seededStore(objects: ObjectStore = new InMemoryObjectStore(), expiry = expired()) {
  const store = new ConnectionStore(objects, scope);
  await store.replaceCredential(key, 0, new CredentialMaterial({ accessToken: 'old', refreshToken: 'renewal' }, expiry, 'oauth2'));
  return store;
}

async function authorizationStore(deadline: Date) {
  const store = new ConnectionStore(new InMemoryObjectStore(), scope);
  const resultKey = `${connectionPath(scope, key)}/exchange-results/authorization-1`;
  const mutation = { kind: 'AUTHORIZING', invocationId: 'invocation-1', id: 'authorization-1', fence: 1, baseRevision: 0, deadline: deadline.toISOString(), resultKey };
  const record = connectionRecord(scope, key, 1, 1, 'AUTHORIZING', undefined, 'oauth2', undefined, mutation);
  await store.objects.createObject(`${connectionPath(scope, key)}/head`, Buffer.from(encodeGoCompatibleJSON(record)));
  return { store, resultKey };
}

describe('project credential refresh', () => {
  it('resolves non-refreshing material and rejects a mismatched call', async () => {
    const store = await seededStore(new InMemoryObjectStore(), future());
    const provider = new CredentialProvider(store, key, decode);
    expect((await provider.resolve(call)).accessToken).toBe('old');
    await expect(provider.resolve({ ...call, connection: { provider: 'sample', name: 'other' } })).rejects.toThrow('differs');
    const material = (await store.readCredentialMaterial(key)).material;
    expect(() => JSON.stringify(material)).toThrow('cannot be serialized');
  });

  it('admits exactly one provider refresh and lets an observer read the published result', async () => {
    const store = await seededStore();
    const first = new RefreshingCredentialProvider(store, key, decode, encode);
    const second = new RefreshingCredentialProvider(store, key, decode, encode);
    let release!: () => void;
    let started!: () => void;
    const didStart = new Promise<void>(resolve => { started = resolve; });
    const gate = new Promise<void>(resolve => { release = resolve; });
    let refreshes = 0;
    const refreshDriver = driver(async state => {
      refreshes++;
      started();
      await gate;
      return { credentials: { accessToken: 'new', refreshToken: state.credentials.refreshToken }, expiresAt: future() };
    });
    const owner = first.resolveWithRefresh(call, refreshDriver);
    await didStart;
    const observer = second.resolveWithRefresh(call, { ...refreshDriver, refreshRequired: state => state.credentials.accessToken !== 'new' });
    release();
    expect((await owner).accessToken).toBe('new');
    expect((await observer).accessToken).toBe('new');
    expect(refreshes).toBe(1);
    expect((await store.readConnection(key)).status).toBe('READY');
  });

  it('recovers an immutable result after the owner loses the head publication', async () => {
    const memory = new InMemoryObjectStore();
    let shouldLoseReadyWrite = false;
    const objects: ObjectStore = {
      readObject: (...args) => memory.readObject(...args), createObject: (...args) => memory.createObject(...args),
      compareAndSwapObject: async (objectKey, etag, contents) => {
        const parsed: unknown = JSON.parse(Buffer.from(contents).toString());
        if (shouldLoseReadyWrite && objectKey.endsWith('/head') && parsed && typeof parsed === 'object' && 'status' in parsed && parsed.status === 'READY') {
          shouldLoseReadyWrite = false;
          throw new OutcomeUnknownError();
        }
        return memory.compareAndSwapObject(objectKey, etag, contents);
      },
    };
    const store = await seededStore(objects);
    const provider = new RefreshingCredentialProvider(store, key, decode, encode);
    shouldLoseReadyWrite = true;
    await expect(provider.resolveWithRefresh(call, driver(async state => ({ credentials: { accessToken: 'recovered', refreshToken: state.credentials.refreshToken }, expiresAt: future() })))).rejects.toBeInstanceOf(OutcomeUnknownError);
    expect((await store.readConnection(key)).status).toBe('REFRESHING');
    const recovered = await provider.resolveWithRefresh(call, { ...driver(async () => { throw new Error('must not dispatch'); }), refreshRequired: state => state.credentials.accessToken !== 'recovered' });
    expect(recovered.accessToken).toBe('recovered');
    expect((await store.readConnection(key)).status).toBe('READY');
  });

  it('recovers an expired authorization result or fences an abandoned exchange', async () => {
    const pending = await authorizationStore(future());
    const neverRefresh = { ...driver(async () => { throw new Error('must not dispatch'); }), refreshRequired: () => false };
    await expect(new RefreshingCredentialProvider(pending.store, key, decode, encode).resolveWithRefresh(call, neverRefresh)).rejects.toMatchObject({ name: 'ExchangePendingError' });

    const abandoned = await authorizationStore(expired());
    const abandonedProvider = new RefreshingCredentialProvider(abandoned.store, key, decode, encode);
    await expect(abandonedProvider.resolveWithRefresh(call, neverRefresh)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    expect((await abandoned.store.readConnection(key)).status).toBe('REAUTHORIZATION_REQUIRED');

    const completed = await authorizationStore(expired());
    const material = new CredentialMaterial({ accessToken: 'authorized', refreshToken: 'kept' }, future(), 'oauth2');
    const envelope = credentialEnvelope(scope, key, 1, material, 'authorization-1');
    await completed.store.objects.createObject(completed.resultKey, Buffer.from(encodeGoCompatibleJSON(envelope)));
    const completedProvider = new RefreshingCredentialProvider(completed.store, key, decode, encode);
    expect((await completedProvider.resolveWithRefresh(call, neverRefresh)).accessToken).toBe('authorized');
    expect((await completed.store.readConnection(key)).status).toBe('READY');
  });

  it('reloads a changed head when two observers recover one expired refresh', async () => {
    const memory = new InMemoryObjectStore();
    const headKey = `${connectionPath(scope, key)}/head`;
    let shouldSynchronizeRecovery = false;
    let headReads = 0;
    let readyWrites = 0;
    let releaseHeadReads!: () => void;
    let releaseReadyWrites!: () => void;
    const headReadGate = new Promise<void>(resolve => { releaseHeadReads = resolve; });
    const readyWriteGate = new Promise<void>(resolve => { releaseReadyWrites = resolve; });
    const objects: ObjectStore = {
      async readObject(objectKey, version) {
        const object = await memory.readObject(objectKey, version);
        if (shouldSynchronizeRecovery && objectKey === headKey && ++headReads <= 2) {
          if (headReads === 2) releaseHeadReads();
          await headReadGate;
        }
        return object;
      },
      createObject: (objectKey, contents) => memory.createObject(objectKey, contents),
      async compareAndSwapObject(objectKey, etag, contents) {
        const document: unknown = JSON.parse(Buffer.from(contents).toString());
        if (shouldSynchronizeRecovery && objectKey === headKey && document && typeof document === 'object' && 'status' in document && document.status === 'READY' && ++readyWrites <= 2) {
          if (readyWrites === 2) releaseReadyWrites();
          await readyWriteGate;
        }
        return memory.compareAndSwapObject(objectKey, etag, contents);
      },
    };
    const store = await seededStore(objects, future());
    const { record, object } = await store.readRecord(key);
    const mutation = { kind: 'REFRESHING', invocationId: 'invocation-1', id: 'refresh-1', fence: record.fence + 1, baseRevision: record.revision, deadline: expired().toISOString(), resultKey: `${connectionPath(scope, key)}/exchange-results/refresh-1` };
    // A refresh admission retains the referenced credential's expiry until a replacement is published.
    const retainedExpiry = record.expiresAt ? new Date(record.expiresAt) : undefined;
    const refreshing = connectionRecord(scope, key, record.revision + 1, record.fence + 1, 'REFRESHING', retainedExpiry, record.authMethod, record.credential, mutation);
    expect(Date.parse(refreshing.expiresAt!)).toBe(Date.parse(record.expiresAt!));
    await store.writeHead(key, object, refreshing);
    shouldSynchronizeRecovery = true;
    const neverRefresh = { ...driver(async () => { throw new Error('must not dispatch'); }), refreshRequired: () => false };
    const first = new RefreshingCredentialProvider(store, key, decode, encode);
    const second = new RefreshingCredentialProvider(store, key, decode, encode);
    const [firstResult, secondResult] = await Promise.all([first.resolveWithRefresh(call, neverRefresh), second.resolveWithRefresh(call, neverRefresh)]);
    expect(firstResult).toEqual(secondResult);
    expect(firstResult.accessToken).toBe('old');
    expect(readyWrites).toBe(2);
    expect((await store.readConnection(key)).status).toBe('READY');
  });

  it('fences a terminal OAuth invalid_grant with one shared error class', async () => {
    const store = await seededStore();
    const provider = new RefreshingCredentialProvider(store, key, decode, encode);
    let requests = 0;
    const endpoint = new TokenEndpoint({ providerName: 'fixture', url: 'https://example.test/token', terminalErrorCodes: ['invalid_grant'], fetch: async () => {
      requests++;
      return new Response('{"error":"invalid_grant"}', { status: 400, headers: { 'Content-Type': 'application/json' } });
    } });
    const refreshDriver = driver(async state => {
      const response = await endpoint.exchangeRefreshToken({ id: 'client', authenticationMethod: 'none' }, new SecretString(state.credentials.refreshToken));
      return { credentials: { accessToken: response.accessToken.reveal(), refreshToken: response.nextRefreshToken(new SecretString(state.credentials.refreshToken)).reveal() }, expiresAt: future() };
    });
    await expect(provider.resolveWithRefresh(call, refreshDriver)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    expect((await store.readConnection(key)).status).toBe('REAUTHORIZATION_REQUIRED');
    await expect(provider.resolveWithRefresh(call, refreshDriver)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    expect(requests).toBe(1);
  });

  it('fences terminal reauthorization and restores prior material after a transient failure', async () => {
    const store = await seededStore();
    const provider = new RefreshingCredentialProvider(store, key, decode, encode);
    await expect(provider.resolveWithRefresh(call, driver(async () => { throw new Error('temporary token endpoint failure'); }))).rejects.toBeInstanceOf(RefreshFailedError);
    expect((await store.readConnection(key)).status).toBe('READY');
    expect((await store.readCredentialMaterial(key)).material.credentials.accessToken).toBe('old');
    await expect(provider.resolveWithRefresh(call, driver(async () => { throw new ReauthorizationRequiredError(); }))).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    expect((await store.readConnection(key)).status).toBe('REAUTHORIZATION_REQUIRED');
    await expect(provider.resolveWithRefresh(call, driver(async () => { throw new Error('must not dispatch'); }))).rejects.toBeInstanceOf(ReauthorizationRequiredError);
  });

  it('forces a rejection refresh only after authoritative expiry', async () => {
    const store = await seededStore(new InMemoryObjectStore(), future());
    const provider = new RefreshingCredentialProvider(store, key, decode, encode);
    await expect(provider.resolveAfterRejection(call, driver(async () => ({ credentials: { accessToken: 'new', refreshToken: 'renewal' }, expiresAt: future() })))).rejects.toBeInstanceOf(RejectedCredentialNotRefreshedError);
    await store.replaceCredential(key, 1, new CredentialMaterial({ accessToken: 'old', refreshToken: 'renewal' }, expired(), 'oauth2'));
    expect((await provider.resolveAfterRejection(call, driver(async () => ({ credentials: { accessToken: 'new', refreshToken: 'renewal' }, expiresAt: future() })))).accessToken).toBe('new');
  });

  it('writes local Dex Web refreshed credentials back only while the file holds the imported value', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'sdkts-refresh-'));
    const path = join(directory, 'connections.json');
    try {
      const document = { schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1', connections: [{ connectorId: 'sample', connectionName: 'default', modulePath: 'sample', provider: 'sample', authMethodId: 'oauth2', configuration: {}, credentials: { accessToken: 'old', refreshToken: 'renewal' }, credentialExpiresAt: expired().toISOString(), customDexWebField: 'preserve' }] };
      await writeFile(path, JSON.stringify(document), { mode: 0o600 });
      const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
      const provider = new RefreshingCredentialProvider(project.connections, key, decode, encode);
      expect((await provider.resolveWithRefresh(call, driver(async state => ({ credentials: { accessToken: 'new', refreshToken: state.credentials.refreshToken }, expiresAt: future() })))).accessToken).toBe('new');
      const saved = JSON.parse(await readFile(path, 'utf8')) as typeof document;
      expect(saved.connections[0]?.credentials).toEqual({ accessToken: 'new', refreshToken: 'renewal' });
      expect(saved.connections[0]?.customDexWebField).toBe('preserve');
      expect((await stat(path)).mode & 0o777).toBe(0o600);
    } finally { await rm(directory, { recursive: true, force: true }); }
  });

  it('keeps a newer Dex Web file value when a refresh races with a user save', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'sdkts-refresh-race-'));
    const path = join(directory, 'connections.json');
    try {
      const document = { schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1', connections: [{ connectorId: 'sample', connectionName: 'default', modulePath: 'sample', provider: 'sample', configuration: {}, credentials: { accessToken: 'old', refreshToken: 'renewal' }, credentialExpiresAt: expired().toISOString() }] };
      await writeFile(path, JSON.stringify(document), { mode: 0o600 });
      const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
      const provider = new RefreshingCredentialProvider(project.connections, key, decode, encode);
      await expect(provider.resolveWithRefresh(call, driver(async () => {
        document.connections[0]!.credentials = { accessToken: 'newer-from-web', refreshToken: 'newer-renewal' };
        document.connections[0]!.credentialExpiresAt = future().toISOString();
        await writeFile(path, JSON.stringify(document), { mode: 0o600 });
        return { credentials: { accessToken: 'stale-refresh', refreshToken: 'renewal' }, expiresAt: future() };
      }))).rejects.toThrow();
      const saved = JSON.parse(await readFile(path, 'utf8')) as typeof document;
      expect(saved.connections[0]?.credentials.accessToken).toBe('newer-from-web');
      expect((await project.connections.readCredentialMaterial(key)).material.credentials.accessToken).toBe('newer-from-web');
    } finally { await rm(directory, { recursive: true, force: true }); }
  });
});
