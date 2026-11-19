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
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { expect, it } from 'vitest';
import { GuGuDataClient } from '../../src/client.js';
import { newConnection, type Credentials } from '../../src/index.js';
import { ConnectionName, GuGuDataInspectionFlow } from '../../examples/basic/flow.js';

it('retries a provider 503 and persists the result through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((_request, response) => {
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ DataStatus: { StatusCode: 100 }, Data: { SegmentaryText: '计算机,科学' } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('GuGuData fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-gugudata-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { appKey: new SecretString('test-gugudata-app-key') };
  const provider = new GuGuDataClient({}, { resolve: async () => credentials }, { send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}/text/segment${new URL(request.url).search}`, addressPolicy: 'privateNetworkHTTP' }) });
  const connection = newConnection(provider, { provider: 'gugudata', name: ConnectionName });
  const flow = new GuGuDataInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `gugudata-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { text: '计算机科学' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({ segmentedText: '计算机,科学' });
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
