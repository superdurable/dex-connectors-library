// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { OneDriveClient } from '../src/client.js';
import type { Credentials } from '../src/generated/models.js';
import { HeartbeatContext, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

it('keeps a leading UTF-8 BOM in readFileText like Go', async () => {
  const connection = { provider: 'microsoft', name: 'onedrive-files' };
  const credentials: Credentials = { accessToken: new SecretString('token') };
  const driver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false,
    refresh: async () => { throw new Error('unexpected refresh'); } };
  const client = new OneDriveClient({ endpoint: 'http://127.0.0.1:12345' },
    new StaticCredentialProvider([[connection, credentials]]), { refreshDriver: driver,
      transport: { send: async request => request.url.endsWith('/content')
        ? { status: 200, headers: {}, body: Buffer.from('\uFEFFhello') }
        : { status: 200, headers: {}, body: Buffer.from(JSON.stringify({ id: 'file-1', name: 'report.txt', size: 8,
          file: { mimeType: 'text/plain' } })) } } });
  const call = scenarioCall({ callId: 'onedrive-bom', operation: 'readFileText' } as Scenario,
    connection, 'microsoft-onedrive', new HeartbeatContext(undefined)) as Call;
  const result = await client.readFileText().invoke(call, { driveId: 'teamDriveKey-2', itemId: 'file-1' });
  expect(result).toMatchObject({ kind: 'branch', branch: 'read', value: { text: '\uFEFFhello', byteCount: 8 } });
});

it('does not resend after a 401 with an invalid replacement credential', async () => {
  const credentials: Credentials = { accessToken: new SecretString('token') };
  let requests = 0;
  const source = { resolve: async () => credentials, resolveWithRefresh: async () => credentials,
    resolveAfterRejection: async () => ({ ...credentials, accessToken: new SecretString('bad token') }) };
  const client = new OneDriveClient({ endpoint: 'http://127.0.0.1:12345' }, source, {
    transport: { send: async () => { requests++; return { status: 401, headers: {}, body: Buffer.from('{}') }; } },
  });
  const call = scenarioCall({ callId: 'onedrive-invalid-refresh', operation: 'getFile' } as Scenario,
    { provider: 'microsoft', name: 'onedrive-files' }, 'microsoft-onedrive', new HeartbeatContext(undefined)) as Call;
  const result = await client.getFile().invoke(call, { driveId: 'teamDriveKey-2', itemId: 'file-1' });
  expect(result).toMatchObject({ kind: 'branch', branch: 'providerRejected' });
  expect(requests).toBe(1);
});
