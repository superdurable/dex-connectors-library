// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { YijingtongClient } from '../../src/client.js';
import { newConnection, type Credentials } from '../../src/index.js';
import { ConnectionName, YijingtongInspectionFlow } from '../../examples/basic/flow.js';

it('retries provider 503 and persists the result through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const providerBody = new TextEncoder().encode(JSON.stringify({ error: 'T', data: [{ nums: 'US01', pname: 'US Warehouse', country: 'United States', private: 'must not persist' }] }));
  const directory = await mkdtemp(join(tmpdir(), 'dex-yijingtong-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { token: new SecretString('REDACTED_YIJINGTONG_TOKEN') };
  const provider = new YijingtongClient({ baseUrl: 'https://tenant.example' }, { resolve: async () => credentials }, { send: async () => {
    attempts++;
    return attempts === 1 ? { status: 503, headers: {}, body: new Uint8Array() } : { status: 200, headers: {}, body: providerBody };
  } });
  const connection = newConnection(provider, { provider: 'yijingtong', name: ConnectionName });
  const flow = new YijingtongInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `yijingtong-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({ warehouses: [{ id: 'US01', name: 'US Warehouse', country: 'United States' }] });
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
