// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createCall } from '../dist/index.js';
import { CredentialBrokerClient, InMemoryObjectStore, LoadedProject, newRefreshingCredentialProvider } from '../dist/projectconfig/index.js';

const broker = new CredentialBrokerClient('https://broker.example.test/read', async () => 'test-scope-token', async () =>
  Response.json({ status: 'READY', revision: 1, credential: { accessToken: 'shared-access' }, expiresAt: new Date(Date.now() + 3600_000).toISOString() }));
const project = new LoadedProject({
  schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha3',
  scope: { projectId: 'example', kind: 'live' }, revision: 1,
  connections: [{ connectorId: 'example', connectionName: 'shared', modulePath: 'example', provider: 'example',
    sharedConnectionId: 'host-selection', configuration: {} }],
  triggerBindings: [], operationConfigurations: [],
  credentialBroker: { endpoint: 'https://broker.example.test/read', tokenSecretRef: {
    key: `projects/example/live/app-secrets/DEX_CREDENTIAL_BROKER_TOKEN/${'a'.repeat(32)}`,
    version: 'v1', digest: `sha256:${'b'.repeat(64)}` } },
}, undefined, new InMemoryObjectStore(), undefined, broker);

const provider = newRefreshingCredentialProvider(project.connections, { connectorId: 'example', connectionName: 'shared' },
  value => {
    if (typeof value.accessToken !== 'string' || typeof value.refreshToken !== 'string') throw new TypeError();
    return { accessToken: value.accessToken, refreshToken: value.refreshToken };
  }, value => ({ ...value }),
  value => {
    if (typeof value.accessToken !== 'string') throw new TypeError();
    return { accessToken: value.accessToken, refreshToken: '' };
  });
const context = { flowId: 'example-flow', stepExecutionId: 'getItem:1', attempt: 1 };
const call = createCall(context, { provider: 'example', name: 'shared' }, { connectorId: 'example', operationId: 'getItem' });
const credential = await provider.resolveWithRefresh(call, {
  refreshRequired: () => true,
  refresh: async () => { throw new Error('a shared credential must refresh through its host'); },
});
if (credential.accessToken !== 'shared-access' || credential.refreshToken !== '') throw new Error('shared credential example failed');
