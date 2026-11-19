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
import { KlaviyoClient } from '../../src/client.js';
import { newConnection, type EventAcceptance } from '../../src/index.js';
import { ConnectionName, ProfileEventFlow } from '../../examples/profile-event/flow/workflow.js';

it('runs profile lookup and event submission through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Klaviyo-API-Key pk_test_key') { response.writeHead(401).end(); return; }
    if (request.method === 'GET' && request.url === '/api/profiles/profile-1') {
      response.writeHead(200, { 'Content-Type': 'application/vnd.api+json' });
      response.end(JSON.stringify({ data: { type: 'profile', id: 'profile-1', attributes: { email: 'member@meridian.co' } } }));
      return;
    }
    if (request.method === 'POST' && request.url === '/api/events') {
      const chunks: Buffer[] = [];
      request.on('data', (chunk: Buffer) => chunks.push(chunk));
      request.on('end', () => {
        const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { data: { attributes: { unique_id: string; profile: { data: { attributes: { email: string } } } } } };
        expect(body.data.attributes.unique_id).toMatch(/^[a-f0-9]{64}$/);
        expect(body.data.attributes.profile.data.attributes.email).toBe('member@meridian.co');
        response.writeHead(202).end();
      });
      return;
    }
    response.writeHead(404).end();
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Klaviyo fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-klaviyo-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new KlaviyoClient({ baseUrl: `http://127.0.0.1:${address.port}/api` }, {
    resolve: async () => ({ privateApiKey: new SecretString('pk_test_key') }),
  });
  const flow = new ProfileEventFlow(newConnection(provider, { provider: 'klaviyo', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `klaviyo-profile-event-${randomUUID()}`;
    await client.startFlow(flow, successId, { profileId: 'profile-1', metricName: 'Signed Up', properties: { source: 'dex-example' } });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<EventAcceptance>().accepted).toBe(true);
    const missingId = `klaviyo-profile-missing-${randomUUID()}`;
    await client.startFlow(flow, missingId, { profileId: 'missing', metricName: 'Signed Up' });
    const missing = await client.waitForFlow(missingId, 60_000);
    expect(missing.status).toBe('failed');
    expect(missing.errorMessage).toContain('notFound');
    expect(requests).toEqual(['GET /api/profiles/profile-1', 'POST /api/events', 'GET /api/profiles/missing']);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
