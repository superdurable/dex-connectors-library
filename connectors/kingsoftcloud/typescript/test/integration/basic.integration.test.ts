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
import { KingsoftCloudClient } from '../../src/client.js';
import { newConnection, type Credentials } from '../../src/index.js';
import { ConnectionName, KingsoftCloudInspectionFlow } from '../../examples/basic/flow.js';

it('retries a provider 503 and persists the result through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((_request, response) => {
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end("{\"ListUserResult\":{\"Users\":{\"member\":[{\"UserId\":\"user-001\",\"UserName\":\"example-user\"}]},\"IsTruncated\":true,\"Marker\":\"another-page=\"}}");
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('KingsoftCloud fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-kingsoftcloud-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { accessKeyId: new SecretString('test-access-key-id'), secretAccessKey: new SecretString('test-secret-access-key') };
  const provider = new KingsoftCloudClient({ baseUrl: `http://127.0.0.1:${address.port}` }, { resolve: async () => credentials });
  const connection = newConnection(provider, { provider: 'kingsoftcloud', name: ConnectionName });
  const flow = new KingsoftCloudInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `kingsoftcloud-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, {"marker":"next-page=","maxItems":25});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({"hasMore":true,"nextMarker":"another-page=","users":[{"userId":"user-001","userName":"example-user"}]});
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
