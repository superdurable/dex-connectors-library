// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { AsyncContext } from '@superdurable/dex';
import { describe, expect, it } from 'vitest';
import { createCall } from '../src/credentials.js';
import { ReauthorizationRequiredError } from '../src/errors.js';
import { ConnectionNotSelectedError, CredentialBrokerClient } from '../src/projectconfig/broker.js';
import { ConnectionStore, CredentialMaterial } from '../src/projectconfig/connections.js';
import { LoadedProject } from '../src/projectconfig/environment.js';
import { CredentialProvider, RefreshingCredentialProvider } from '../src/projectconfig/provider.js';
import { InMemoryObjectStore } from '../src/projectconfig/storage.js';

const scope = { projectId: 'test', kind: 'live' } as const;
const ordinaryKey = { connectorId: 'sample', connectionName: 'ordinary' };
const sharedKey = { connectorId: 'sample', connectionName: 'shared' };
const context = { flowId: 'flow-1', stepExecutionId: 'getItem:1', attempt: 1, cancellationSignal: new AbortController().signal } as AsyncContext;
const ordinaryCall = createCall(context, { provider: 'sample', name: 'ordinary' }, { connectorId: 'sample', operationId: 'getItem' });
const sharedCall = createCall(context, { provider: 'sample', name: 'shared' }, { connectorId: 'sample', operationId: 'getItem' });
const decode = (document: Record<string, unknown>) => {
  if (typeof document.accessToken !== 'string' || typeof document.refreshToken !== 'string') throw new TypeError();
  return { accessToken: document.accessToken, refreshToken: document.refreshToken };
};
const decodeShared = (document: Record<string, unknown>) => {
  if (typeof document.accessToken !== 'string') throw new TypeError();
  return { accessToken: document.accessToken, refreshToken: '' };
};
const encode = (credential: ReturnType<typeof decode>) => ({ ...credential });

function projectWithBroker(answers: Array<Record<string, unknown>>) {
  const requests: Array<Record<string, unknown>> = [];
  const broker = new CredentialBrokerClient('https://broker.example.test/read', async () => 'scope-token', async (_url, options) => {
    requests.push(JSON.parse(String(options?.body)) as Record<string, unknown>);
    return Response.json(answers.shift() ?? { status: 'CONNECTION_NOT_SELECTED' });
  });
  const objects = new InMemoryObjectStore();
  const configuration = {
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha3' as const, scope, revision: 1,
    connections: [
      { ...ordinaryKey, modulePath: 'sample', provider: 'sample', configuration: {} },
      { ...sharedKey, modulePath: 'sample', provider: 'sample', sharedConnectionId: 'host-selection', configuration: {} },
    ], triggerBindings: [], operationConfigurations: [],
    credentialBroker: { endpoint: 'https://broker.example.test/read', tokenSecretRef: {
      key: `projects/test/live/app-secrets/DEX_CREDENTIAL_BROKER_TOKEN/${'a'.repeat(32)}`,
      version: 'v1', digest: `sha256:${'b'.repeat(64)}`,
    } },
  };
  return { project: new LoadedProject(configuration, undefined, objects, undefined, broker), objects, requests };
}

describe('project shared credential providers', () => {
  it('routes only selected shared entries through the broker and never calls their refresh driver', async () => {
    const { project, objects, requests } = projectWithBroker([{ status: 'READY', credential: { accessToken: 'host-access' }, revision: 1, expiresAt: new Date(Date.now() + 3600_000).toISOString() }]);
    await new ConnectionStore(objects, scope).replaceCredential(ordinaryKey, 0, new CredentialMaterial({ accessToken: 'ordinary-access', refreshToken: 'renewal' }, undefined, 'oauth2'));
    const ordinary = new CredentialProvider(project.connections, ordinaryKey, decode);
    expect((await ordinary.resolve(ordinaryCall)).accessToken).toBe('ordinary-access');
    expect(requests).toHaveLength(0);
    const shared = new RefreshingCredentialProvider(project.connections, sharedKey, decode, encode, decodeShared);
    let refreshes = 0;
    const driver = { refreshRequired: () => true, refresh: async () => { refreshes++; throw new Error('shared credential cannot refresh locally'); } };
    expect((await shared.resolveWithRefresh(sharedCall, driver)).accessToken).toBe('host-access');
    expect((await shared.resolve(sharedCall)).refreshToken).toBe('');
    expect(requests).toEqual([{ sharedConnectionId: 'host-selection' }]);
    expect(refreshes).toBe(0);
    await expect(project.connections.readConnection(sharedKey)).rejects.toThrow();
  });

  it('routes a rejected shared revision to the broker without invoking refresh', async () => {
    const { project, requests } = projectWithBroker([
      { status: 'READY', credential: { accessToken: 'first' }, revision: 1, expiresAt: new Date(Date.now() + 3600_000).toISOString() },
      { status: 'READY', credential: { accessToken: 'second' }, revision: 2, expiresAt: new Date(Date.now() + 3600_000).toISOString() },
    ]);
    const shared = new RefreshingCredentialProvider(project.connections, sharedKey, decode, encode, decodeShared);
    const driver = { refreshRequired: () => true, refresh: async () => { throw new Error('must not refresh'); } };
    expect((await shared.resolveWithRefresh(sharedCall, driver)).accessToken).toBe('first');
    expect((await shared.resolveAfterRejection(sharedCall, driver)).accessToken).toBe('second');
    expect(requests).toEqual([{ sharedConnectionId: 'host-selection' }, { sharedConnectionId: 'host-selection', rejectedRevision: 1 }]);
  });

  it('preserves broker terminal statuses and rejects ordinary decoding of host-kept secrets', async () => {
    for (const [status, expected] of [
      ['REAUTHORIZATION_REQUIRED', ReauthorizationRequiredError],
      ['CONNECTION_NOT_SELECTED', ConnectionNotSelectedError],
    ] as const) {
      const { project } = projectWithBroker([{ status }]);
      const shared = new RefreshingCredentialProvider(project.connections, sharedKey, decode, encode, decodeShared);
      await expect(shared.resolveWithRefresh(sharedCall, { refreshRequired: () => true, refresh: async () => { throw new Error('must not dispatch'); } })).rejects.toBeInstanceOf(expected);
    }
    const { project } = projectWithBroker([{ status: 'READY', credential: { accessToken: 'host-access' }, revision: 1 }]);
    const provider = new CredentialProvider(project.connections, sharedKey, decode);
    await expect(provider.resolve(sharedCall)).rejects.toThrow('does not support shared credentials');
  });
});
