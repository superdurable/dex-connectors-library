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
import { MailgunClient, newConnection, type EventsOutput } from '../../src/index.js';
import { ConnectionName, GetEventsFlow } from '../../examples/get-events/flow/workflow.js';

it('routes a Mailgun page through real Dex and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.method !== 'GET' || !request.url?.startsWith('/v3/example.test/events')) { response.writeHead(404).end(); return; }
    if (request.headers.authorization !== `Basic ${Buffer.from('api:test-key-123').toString('base64')}`) { response.writeHead(401).end(); return; }
    if (request.url.includes('limit=13')) { response.writeHead(403).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end('{"items":[{"id":"event-1","event":"delivered","timestamp":1793100000.5}]}');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Mailgun fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-mailgun-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new MailgunClient({ baseUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => ({ apiKey: new SecretString('test-key-123') }),
  });
  const flow = new GetEventsFlow(newConnection(provider, { provider: 'mailgun', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `mailgun-events-${randomUUID()}`;
    await client.startFlow(flow, successId, { domain: 'example.test', limit: 20 });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<EventsOutput>().events?.[0]?.id).toBe('event-1');
    const rejectedId = `mailgun-events-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { domain: 'example.test', limit: 13 });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
