// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, it } from 'vitest';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { EntraClient, newConnection } from '../../src/index.js';
import { ConnectionName, UserLookupFlow } from './flow.js';

it('reads an account through a real Dex Worker and fails an unwired missing branch', async () => {
  const reference = { provider: 'microsoft', name: ConnectionName };
  const credentials = new StaticCredentialProvider([[reference, { authMethodId: 'microsoft-oauth', clientId: 'inert-client', clientSecret: new SecretString('inert-secret'), accessToken: new SecretString('inert-token'), refreshToken: new SecretString('inert-refresh') }]]);
  const send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const isMissing = request.url.includes('absent%40contoso.com') || request.url.includes('absent@contoso.com');
    return { status: isMissing ? 404 : 200, headers: {}, body: Buffer.from(isMissing ? '{}' : JSON.stringify({ id: '11111111-2222-4333-8444-555555555555', userPrincipalName: 'ada@contoso.com', accountEnabled: true })) };
  };
  const flow = new UserLookupFlow(newConnection(new EntraClient({}, credentials, { send }), reference));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'entra-dex-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS ?? '127.0.0.1:8801';
  const worker = new Worker(registry, cache, { serverAddress: address });
  const client = new Client(registry, cache, { serverAddress: address });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 15_000 });
    const foundId = `entra-found-${randomUUID()}`;
    const missingId = `entra-missing-${randomUUID()}`;
    await client.startFlow(flow, foundId, { userKey: 'ada@contoso.com' });
    await client.startFlow(flow, missingId, { userKey: 'absent@contoso.com' });
    const found = await client.waitForFlow(foundId, 30_000);
    const missing = await client.waitForFlow(missingId, 30_000);
    expect(found.status).toBe('completed');
    expect(found.singleOutput()).toMatchObject({ branch: 'found', value: { id: '11111111-2222-4333-8444-555555555555' } });
    expect(missing.status).toBe('failed');
  } finally {
    await client.close();
    await worker.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 60_000);
