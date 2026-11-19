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
import { GorgiasClient } from '../../src/client.js';
import { newConnection, type TicketDetails } from '../../src/index.js';
import { ConnectionName, ReadTicketFlow } from '../../examples/read-ticket/flow/workflow.js';

it('reads a ticket through real Dex and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== `Basic ${Buffer.from('agent@acme.example.com:gorgiasTestKey0123456789abcdef').toString('base64')}`) { response.writeHead(401).end(); return; }
    if (request.method !== 'GET') { response.writeHead(405).end(); return; }
    if (request.url === '/api/tickets/7002') { response.writeHead(403, { 'Content-Type': 'application/json' }).end('{}'); return; }
    if (request.url !== '/api/tickets/7001') { response.writeHead(404).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ id: 7001, subject: 'Duplicate charge', status: 'open', priority: 'normal', customer: { id: 501, email: 'ada@example.com' }, tags: [{ name: 'billing' }], messages: [], created_datetime: '2026-10-20T09:00:00Z' }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Gorgias fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-gorgias-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new GorgiasClient({ domain: 'acme' }, { resolve: async () => ({ email: 'agent@acme.example.com', apiKey: new SecretString('gorgiasTestKey0123456789abcdef') }) }, { apiBaseURL: `http://127.0.0.1:${address.port}/api` });
  const flow = new ReadTicketFlow(newConnection(provider, { provider: 'gorgias', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successID = `gorgias-read-${randomUUID()}`;
    await client.startFlow(flow, successID, { ticketId: 7001 });
    const success = await client.waitForFlow(successID, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<TicketDetails>().ticket.id).toBe(7001);
    const rejectionID = `gorgias-read-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionID, { ticketId: 7002 });
    const rejected = await client.waitForFlow(rejectionID, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toEqual(['GET /api/tickets/7001', 'GET /api/tickets/7002']);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
