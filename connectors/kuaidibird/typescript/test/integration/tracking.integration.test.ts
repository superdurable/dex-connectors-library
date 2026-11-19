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
import { KuaidibirdClient } from '../../src/client.js';
import { newConnection, type QueryTrackingOutput } from '../../src/index.js';

it('retries KuaidiBird 503 through a real Dex Worker and persists the observation', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'POST' || request.url !== '/Ebusiness/EbusinessOrderHandle.aspx') {
      response.writeHead(400).end(); return;
    }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ EBusinessID: '1237100', ShipperCode: 'YD', LogisticCode: '123456789012', Success: true, State: '3', Traces: [{ AcceptTime: '2026-11-13 10:00:00', AcceptStation: 'Delivered' }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('KuaidiBird fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-kuaidibird-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new KuaidibirdClient({ baseUrl: `http://127.0.0.1:${address.port}` }, { resolve: async () => ({ apiKey: new SecretString('test-api-key'), businessId: new SecretString('1237100') }) });
  const flow = new TrackingFlow(newConnection(provider, { provider: 'kuaidibird', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `kuaidibird-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { carrierCode: 'YD', trackingNumber: '123456789012' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<QueryTrackingOutput>()).toEqual({ carrierCode: 'YD', trackingNumber: '123456789012', state: '3', events: [{ time: '2026-11-13 10:00:00', content: 'Delivered' }] });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
