// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { SecretString } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { ConnectionName, DepartmentCatalogFlow } from '../../examples/department-list/flow.js';
import { HesiClient } from '../../src/client.js';
import { newConnection, type ListDepartmentsOutput } from '../../src/index.js';

it('retries Hesi department 503 through a real Dex Worker and persists the page', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  let departmentRequests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method === 'POST' && request.url === '/api/openapi/v1/auth/getAccessToken' && request.headers.accept === 'application/json') {
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ value: { accessToken: 'short-lived-token' } })); return;
    }
    if (request.method !== 'GET' || request.url !== '/api/openapi/v1/departments?accessToken=short-lived-token&start=2&count=10' || request.headers.accept !== 'application/json') {
      response.writeHead(400).end(); return;
    }
    departmentRequests++;
    if (departmentRequests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ count: 4, items: [{ id: 'corp:dept', name: 'Example Department', code: '001', active: true }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Hesi fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-hesi-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new HesiClient({}, { resolve: async () => ({ appKey: new SecretString('test-app-key'), appSecurity: new SecretString('test-app-security') }) }, {
    send: request => { const original = new URL(request.url); return sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}${original.pathname}${original.search}`, addressPolicy: 'privateNetworkHTTP' }); },
  });
  const flow = new DepartmentCatalogFlow(newConnection(provider, { provider: 'hesi', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `hesi-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { start: 2, count: 10 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<ListDepartmentsOutput>()).toEqual({ departments: [{ id: 'corp:dept', name: 'Example Department', code: '001', active: true }], total: 4, nextStart: 3 });
    expect(requests).toBe(4);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
