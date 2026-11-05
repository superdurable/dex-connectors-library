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
import { FrontClient } from '../../src/client.js';
import { newConnection, type FindContactByEmailOutput } from '../../src/index.js';
import { ConnectionName, FindContactByEmailFlow } from '../../examples/find-contact/flow/workflow.js';

it('finds a contact through a real Dex Worker and fails an unwired denial', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer front-unit-test-token-0123456789') { response.writeHead(401).end(); return; }
    if (request.url?.includes('denied@')) { response.writeHead(403).end('{}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ id: 'crd_1y8sp71', handles: [{ handle: 'jane@acme.example.com', source: 'email' }], updated_at: 1767225600 }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Front fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-front-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new FrontClient({}, { resolve: async () => ({ apiToken: new SecretString('front-unit-test-token-0123456789') }) }, { apiBaseURL: `http://127.0.0.1:${address.port}` });
  const flow = new FindContactByEmailFlow(newConnection(provider, { provider: 'front', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `front-contact-${randomUUID()}`;
    await client.startFlow(flow, successId, { email: 'jane@acme.example.com' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<FindContactByEmailOutput>().contacts?.[0]?.id).toBe('crd_1y8sp71');
    const deniedId = `front-contact-denied-${randomUUID()}`;
    await client.startFlow(flow, deniedId, { email: 'denied@acme.example.com' });
    const denied = await client.waitForFlow(deniedId, 60_000);
    expect(denied.status).toBe('failed');
    expect(denied.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
