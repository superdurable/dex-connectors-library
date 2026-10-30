// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { SecretString, type Call, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { CredentialBrokerClient, InMemoryObjectStore, LoadedProject, loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../src/index.js';
import type { Credentials } from '../src/models.js';

describe('Gmail project connection', () => {
  it('reads a host-minted shared token without receiving host-kept refresh material', async () => {
    const broker = new CredentialBrokerClient('https://broker.example.test/read', async () => 'scope-token', async () =>
      Response.json({ status: 'READY', revision: 1, credential: { auth_method: 'google-oauth', oauth_client_id: 'client', access_token: 'shared-access', primary_email: 'owner@example.com' }, expiresAt: new Date(Date.now() + 3_600_000).toISOString() }));
    const project = new LoadedProject({
      schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha3', scope: { projectId: 'gmail-example', kind: 'live' }, revision: 1,
      connections: [{ connectorId: 'gmail', connectionName: 'gmail-inbox', modulePath: '@superdurable/connector-google-gmail', provider: 'google', sharedConnectionId: 'host-selection', configuration: {} }],
      triggerBindings: [], operationConfigurations: [],
      credentialBroker: { endpoint: 'https://broker.example.test/read', tokenSecretRef: { key: `projects/gmail-example/live/app-secrets/DEX_CREDENTIAL_BROKER_TOKEN/${'a'.repeat(32)}`, version: 'v1', digest: `sha256:${'b'.repeat(64)}` } },
    }, undefined, new InMemoryObjectStore(), undefined, broker);
    const connection = newProjectConnection(project, 'gmail-inbox', { transport: async request => {
      expect(request.headers?.Authorization).toBe('Bearer shared-access');
      return { status: 200, headers: {}, body: Buffer.from(JSON.stringify({ id: 'shared-message', threadId: 'shared-thread', internalDate: '1767312000000' })) };
    } });
    const call = {
      id: '01234567-89ab-5cde-8f01-23456789abcd', connection: { provider: 'google', name: 'gmail-inbox' },
      operation: { connectorId: 'gmail', operationId: 'getMessage' }, context: { cancellationSignal: new AbortController().signal },
    } as Call;
    expect(connection.reference).toEqual({ provider: 'google', name: 'gmail-inbox' });
    expect(await connection.operations.getMessage().invoke(call, { messageId: 'shared-message' })).toMatchObject({ branch: 'read', value: { messageId: 'shared-message' } });
  });

  it('refreshes through the SDK provider and writes replacement secrets to the private local Dex Web file', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'gmail-ts-project-'));
    const path = join(directory, 'connections.json');
    try {
      await writeFile(path, JSON.stringify({
        schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
        connections: [{ connectorId: 'gmail', connectionName: 'gmail-inbox', modulePath: '@superdurable/connector-google-gmail',
          provider: 'google', authMethodId: 'google-oauth', configuration: { maxResponseBytes: 4096 },
          credentials: { auth_method: 'google-oauth', oauth_client_id: 'client', oauth_client_secret: 'private-client-secret', access_token: 'old-access', refresh_token: 'private-refresh', primary_email: 'owner@example.com' },
          credentialExpiresAt: new Date(Date.now() - 60_000).toISOString() }],
      }), { mode: 0o600 });
      const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
      let refreshCount = 0;
      const driver: CredentialRefreshDriver<Credentials> = {
        refreshRequired: () => false,
        refresh: async state => {
          refreshCount++;
          return { credentials: { ...state.credentials, accessToken: new SecretString('new-access') }, expiresAt: new Date(Date.now() + 3_600_000) };
        },
      };
      const connection = newProjectConnection(project, 'gmail-inbox', {
        refreshDriver: driver,
        transport: async request => {
          expect(request.headers?.Authorization).toBe('Bearer new-access');
          expect(request.maxResponseBytes).toBe(4096);
          return { status: 200, headers: {}, body: Buffer.from(JSON.stringify({ id: 'message-1', threadId: 'thread-1', internalDate: '1767312000000' })) };
        },
      });
      const call = {
        id: '01234567-89ab-5cde-8f01-23456789abcd', connection: { provider: 'google', name: 'gmail-inbox' },
        operation: { connectorId: 'gmail', operationId: 'getMessage' }, context: { cancellationSignal: new AbortController().signal },
      } as Call;
      expect(await connection.operations.getMessage().invoke(call, { messageId: 'message-1' })).toMatchObject({ branch: 'read', value: { messageId: 'message-1' } });
      expect(refreshCount).toBe(1);
      const saved = JSON.parse(await readFile(path, 'utf8')) as { connections: { credentials: Record<string, string> }[] };
      expect(saved.connections[0]?.credentials).toMatchObject({ access_token: 'new-access', refresh_token: 'private-refresh', oauth_client_secret: 'private-client-secret' });
    } finally { await rm(directory, { recursive: true, force: true }); }
  });
});
