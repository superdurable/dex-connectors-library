// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { SecretString } from '@superdurable/connectors-sdk';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { ConnectionName, TrackingFlow } from '../../examples/tracking/flow.js';
import { Kuaidi100Client } from '../../src/client.js';
import { newConnection, type QueryTrackingOutput } from '../../src/index.js';

it('queries Kuaidi100 through a real Dex Worker and persists the observation', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'POST' || request.url !== '/poll/query.do') {
      response.writeHead(400).end(); return;
    }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ status: '200', com: 'yuantong', nu: 'YT1234567890123', state: '0', data: [{ time: '2026-11-13 10:00:00', context: 'Arrived' }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Kuaidi100 fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-kuaidi100-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new Kuaidi100Client({ baseUrl: `http://127.0.0.1:${address.port}` }, { resolve: async () => ({ customer: new SecretString('test-customer'), key: new SecretString('test-key') }) });
  const flow = new TrackingFlow(newConnection(provider, { provider: 'kuaidi100', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `kuaidi100-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { carrierCode: 'yuantong', trackingNumber: 'YT1234567890123', phone: '13800000000' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<QueryTrackingOutput>()).toEqual({ carrierCode: 'yuantong', trackingNumber: 'YT1234567890123', state: '0', events: [{ time: '2026-11-13 10:00:00', content: 'Arrived' }] });
    expect(requests).toBe(1);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
