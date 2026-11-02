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
import { CloudflareClient } from '../../src/client.js';
import { newConnection, type ZonesPage } from '../../src/index.js';
import { CloudflareLookupFlow, ConnectionName } from '../../examples/cloudflare-lookup/flow/workflow.js';

it('lists zones through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  let shouldReject = false;
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.method !== 'GET' || request.url !== '/zones') { response.writeHead(404).end(); return; }
    if (request.headers.authorization !== 'Bearer test-apitoken') { response.writeHead(401).end(); return; }
    response.writeHead(shouldReject ? 403 : 200, { 'Content-Type': 'application/json' });
    response.end(shouldReject ? '{"success":false}' : '{"success":true,"result":[{"id":"zone-meridian","name":"meridian.example","status":"active"}]}');
  });
  let providerPort = 0;
  for (let port = 21201; port <= 21299; port++) {
    try { server.listen(port, '127.0.0.1'); await once(server, 'listening'); providerPort = port; break; }
    catch (error) { if ((error as NodeJS.ErrnoException).code !== 'EADDRINUSE') throw error; }
  }
  if (!providerPort) throw new Error('no available Cloudflare fixture port in 21201-21299');
  const directory = await mkdtemp(join(tmpdir(), 'dex-cloudflare-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new CloudflareClient({ baseUrl: `http://127.0.0.1:${providerPort}` }, {
    resolve: async () => ({ apiToken: new SecretString('test-apitoken') }),
  });
  const flow = new CloudflareLookupFlow(newConnection(provider, { provider: 'cloudflare', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `cloudflare-list-${randomUUID()}`;
    await client.startFlow(flow, successId, {});
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<ZonesPage>().result?.[0]?.id).toBe('zone-meridian');
    shouldReject = true;
    const rejectionId = `cloudflare-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, {});
    const rejection = await client.waitForFlow(rejectionId, 60_000);
    expect(rejection.status).toBe('failed');
    expect(rejection.errorMessage).toContain('providerRejected');
    expect(requests).toEqual(['GET /zones', 'GET /zones']);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
