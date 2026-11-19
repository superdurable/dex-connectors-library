// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { NewayzClient } from '../../src/client.js';
import { newConnection, type Credentials } from '../../src/index.js';
import { ConnectionName, NewayzInspectionFlow } from '../../examples/basic/flow.js';

it('retries provider 503 and persists the result through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const providerBody = new TextEncoder().encode("{\"status\":\"1\",\"infocode\":\"10000\",\"lives\":[{\"province\":\"湖北省\",\"city\":\"武汉市\",\"adcode\":\"420100\",\"weather\":\"晴\",\"temperature\":\"20\",\"winddirection\":\"东风\",\"windpower\":\"2\",\"humidity\":\"50\",\"reporttime\":\"2026-01-01 12:00:00\"}]}");
  const directory = await mkdtemp(join(tmpdir(), 'dex-newayz-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { accessKey: new SecretString("test-newayz-access-key") };
  const provider = new NewayzClient({}, { resolve: async () => credentials }, { send: async () => {
    attempts++;
    return attempts === 1 ? { status: 503, headers: {}, body: new Uint8Array() } : { status: 200, headers: {}, body: providerBody };
  } });
  const connection = newConnection(provider, { provider: 'newayz', name: ConnectionName });
  const flow = new NewayzInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `newayz-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, {"cityCode": "420100"});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({"city": "武汉市", "cityCode": "420100", "humidityPercent": "50", "province": "湖北省", "reportTime": "2026-01-01 12:00:00", "temperatureCelsius": "20", "weather": "晴", "windDirection": "东风", "windPower": "2"});
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
