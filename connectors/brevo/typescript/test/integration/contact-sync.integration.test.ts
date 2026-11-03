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
import { BrevoClient } from '../../src/client.js';
import { newConnection, type Contact } from '../../src/index.js';
import { ConnectionName, ContactSyncFlow } from '../../examples/contact-sync/flow/workflow.js';

it('syncs a contact through real Dex and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers['api-key'] !== 'brevo-test-key') { response.writeHead(401).end(); return; }
    if (request.method === 'POST' && request.url === '/v3/contacts') {
      const chunks: Buffer[] = [];
      request.on('data', (chunk: Buffer) => chunks.push(chunk));
      request.on('end', () => {
        const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { email: string; attributes?: Record<string, string>; updateEnabled: boolean };
        if (body.email === 'reject@meridian.example') { response.writeHead(403).end(); return; }
        if (!body.updateEnabled || body.attributes?.FNAME !== 'Meridian') { response.writeHead(400).end(); return; }
        response.writeHead(201, { 'Content-Type': 'application/json' }).end('{"id":21}');
      });
      return;
    }
    if (request.method === 'GET' && request.url === '/v3/contacts/member@meridian.example') {
      response.writeHead(200, { 'Content-Type': 'application/json' }).end('{"id":21,"email":"member@meridian.example"}');
      return;
    }
    response.writeHead(404).end();
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Brevo fixture has no TCP address');
  const directory = await mkdtemp(join(tmpdir(), 'dex-brevo-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new BrevoClient({ baseUrl: `http://127.0.0.1:${address.port}/v3` }, {
    resolve: async () => ({ apiKey: new SecretString('brevo-test-key') }),
  });
  const flow = new ContactSyncFlow(newConnection(provider, { provider: 'brevo', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `brevo-contact-${randomUUID()}`;
    await client.startFlow(flow, successId, { email: 'member@meridian.example', attributes: { FNAME: 'Meridian' } });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<Contact>()).toEqual({ id: 21, email: 'member@meridian.example' });
    const rejectedId = `brevo-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { email: 'reject@meridian.example' });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toEqual(['POST /v3/contacts', 'GET /v3/contacts/member@meridian.example', 'POST /v3/contacts']);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
