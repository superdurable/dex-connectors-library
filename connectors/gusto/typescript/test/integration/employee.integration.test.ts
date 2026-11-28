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
import { GustoClient } from '../../src/client.js';
import { newConnection, type Employee, type EmployeesPage } from '../../src/index.js';
import { ConnectionName, EmployeeFlow } from '../../examples/employee/flow.js';
import { EmployeeListFlow } from '../../examples/employee/list-flow.js';

it('retries a provider 503 and persists employee through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer synthetic-gusto-token') { response.writeHead(401).end(); return; }
    const requestURL = new URL(request.url ?? '', 'http://127.0.0.1');
    if (requestURL.pathname === '/v1/companies/7d61f73e-a18c-46a5-88e2-2bfa894657d7/employees') {
      if (requestURL.searchParams.get('per') !== '2') { response.writeHead(400).end(); return; }
      const page = requestURL.searchParams.get('page');
      if (page !== '1' && page !== '2') { response.writeHead(400).end(); return; }
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify(page === '1' ? [{
        uuid: '90a68db5-cb3a-4f64-bb60-c0a934ee7600', first_name: 'Ada', last_name: 'Lovelace', email: null,
        company_uuid: '7d61f73e-a18c-46a5-88e2-2bfa894657d7', compensations: [{ rate: 77 }],
      }] : []));
      return;
    }
    if (requestURL.pathname !== '/v1/employees/90a68db5-cb3a-4f64-bb60-c0a934ee7600') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ uuid: "90a68db5-cb3a-4f64-bb60-c0a934ee7600", first_name: 'Ada', last_name: 'Lovelace', email: 'ada@example.com', company_uuid: "7d61f73e-a18c-46a5-88e2-2bfa894657d7" }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Employee fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-gusto-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new GustoClient({ redirectUri: 'https://dex.example/callback', baseUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => ({ oauthClientId: 'synthetic-client', oauthClientSecret: new SecretString('synthetic-secret'), accessToken: new SecretString('synthetic-gusto-token'), refreshToken: new SecretString('synthetic-refresh') }),
  });
  const connection = newConnection(provider, { provider: 'gusto', name: ConnectionName });
  const flow = new EmployeeFlow(connection);
  const pageFlow = new EmployeeListFlow(connection);
  const registry = new Registry([flow, pageFlow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `gusto-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { employeeId: '90a68db5-cb3a-4f64-bb60-c0a934ee7600' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<Employee>().employeeId).toBe('90a68db5-cb3a-4f64-bb60-c0a934ee7600');
    expect(attempts).toBe(2);
    const companyId = '7d61f73e-a18c-46a5-88e2-2bfa894657d7';
    for (const page of [1, 2]) {
      const pageId = `gusto-page-${randomUUID()}`;
      await client.startFlow(pageFlow, pageId, { companyId, page, perPage: 2 });
      const pageResult = await client.waitForFlow(pageId, 30_000);
      expect(pageResult.status, pageResult.errorMessage).toBe('completed');
      expect(pageResult.singleOutput<EmployeesPage>()).toEqual({ employees: page === 1 ? [{
        employeeId: '90a68db5-cb3a-4f64-bb60-c0a934ee7600', firstName: 'Ada', lastName: 'Lovelace', email: '', companyId,
      }] : [] });
    }
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
