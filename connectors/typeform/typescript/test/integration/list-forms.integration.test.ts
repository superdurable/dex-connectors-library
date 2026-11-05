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
import { TypeformClient } from '../../src/client.js';
import { newConnection, type FormPage } from '../../src/index.js';
import { ConnectionName, ListFormsFlow } from '../../examples/list-forms/flow/workflow.js';

it('lists forms through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer test-access-token') { response.writeHead(401).end(); return; }
    if (request.url?.includes('REJECT')) { response.writeHead(403).end('{"code":"DENIED"}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ total_items: 1, page_count: 1, items: [{ id: 'u6nXL7', title: 'Lead intake' }] }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-typeform-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new TypeformClient({}, { resolve: async () => ({ accessToken: new SecretString('test-access-token') }) }, {
    send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}${new URL(request.url).pathname}${new URL(request.url).search}`, addressPolicy: 'privateNetworkHTTP' }),
  });
  const flow = new ListFormsFlow(newConnection(provider, { provider: 'typeform', name: ConnectionName }));
  const registry = new Registry([flow]); const worker = new Worker(registry, cache, { serverAddress: dexAddress }); const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `typeform-list-${randomUUID()}`; await client.startFlow(flow, successId, { search: 'Lead' });
    const success = await client.waitForFlow(successId, 60_000); expect(success.status, success.errorMessage).toBe('completed'); expect(success.singleOutput<FormPage>().forms?.[0]?.id).toBe('u6nXL7');
    const rejectedId = `typeform-list-rejected-${randomUUID()}`; await client.startFlow(flow, rejectedId, { search: 'REJECT' });
    const rejected = await client.waitForFlow(rejectedId, 60_000); expect(rejected.status).toBe('failed'); expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close(); await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
