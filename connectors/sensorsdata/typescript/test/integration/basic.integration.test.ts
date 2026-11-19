// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { SensorsDataClient } from '../../src/client.js';
import { newConnection, type Credentials } from '../../src/index.js';
import { ConnectionName, SensorsDataInspectionFlow } from '../../examples/basic/flow.js';

it('retries a provider 503 and persists the result through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((_request, response) => {
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ code: 'SUCCESS', data: { campaign_details: [{ campaign_name: '测试活动', branch_num: 1, latest_use_time: '2025-01-01 12:00:00' }], total_rows: 6, total_page: 1, page_num: 1, page_size: 20 } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('SensorsData fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-sensorsdata-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { apiKey: new SecretString('k'.repeat(35)) };
  const provider = new SensorsDataClient({ baseUrl: `http://127.0.0.1:${address.port}`, projectName: 'production' }, { resolve: async () => credentials });
  const connection = newConnection(provider, { provider: 'sensorsdata', name: ConnectionName });
  const flow = new SensorsDataInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `sensorsdata-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { pageNum: 1, pageSize: 20 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({ campaigns: [{ name: '测试活动', linkCount: 1, latestUseTime: '2025-01-01 12:00:00' }], totalRows: 6, totalPages: 1, pageNum: 1, pageSize: 20 });
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
