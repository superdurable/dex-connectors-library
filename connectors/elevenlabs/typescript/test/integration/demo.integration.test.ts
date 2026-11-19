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
import { ElevenLabsClient, newConnection, type ListVoicesOutput } from '../../src/index.js';
import { ConnectionName, ElevenLabsDemoFlow } from '../../examples/demo/flow.js';

it('lists voices through a real Dex Worker and fails an unwired provider rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers['xi-api-key'] !== 'test-key-123') { response.writeHead(401).end(); return; }
    if (request.url?.includes('reject')) { response.writeHead(403).end('private provider error'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ voices: [{ voice_id: 'voice_meridian', name: 'Meridian Narrator' }], has_more: false }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-elevenlabs-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new ElevenLabsClient({ baseUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => ({ apiKey: new SecretString('test-key-123') }),
  });
  const flow = new ElevenLabsDemoFlow(newConnection(provider, { provider: 'elevenlabs', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successID = `elevenlabs-success-${randomUUID()}`;
    await client.startFlow(flow, successID, { pageSize: 10 });
    const success = await client.waitForFlow(successID, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<ListVoicesOutput>().voices?.[0]?.voiceId).toBe('voice_meridian');
    const rejectedID = `elevenlabs-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedID, { nextPageToken: 'reject' });
    const rejected = await client.waitForFlow(rejectedID, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
