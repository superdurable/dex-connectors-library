// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { NavInfoClient } from '../../src/client.js';
import { newConnection, type Credentials } from '../../src/index.js';
import { ConnectionName, NavInfoInspectionFlow } from '../../examples/basic/flow.js';

it('retries provider 503 and persists the result through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const providerBody = new TextEncoder().encode("{\"status\":200,\"data\":{\"prov\":\"北京市\",\"city\":\"北京市\",\"adcode\":\"110000\",\"private\":\"private-cover\"},\"msg\":\"private-trace\"}");
  const directory = await mkdtemp(join(tmpdir(), 'dex-navinfo-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { ak: new SecretString("test-secret") };
  const provider = new NavInfoClient({}, { resolve: async () => credentials }, { send: async () => {
    attempts++;
    return attempts === 1 ? { status: 503, headers: {}, body: new Uint8Array() } : { status: 200, headers: {}, body: providerBody };
  } });
  const connection = newConnection(provider, { provider: 'navinfo', name: ConnectionName });
  const flow = new NavInfoInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `navinfo-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, {"ipAddress": "118.28.8.8"});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({"adcode": "110000", "city": "北京市", "province": "北京市"});
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
