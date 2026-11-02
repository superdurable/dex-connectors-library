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
import { BambooHRClient } from '../../src/client.js';
import { newConnection, type EmployeeRecord } from '../../src/index.js';
import { ConnectionName, GetEmployeeFlow } from '../../examples/get-employee/flow/workflow.js';

it('reads an employee through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.method !== 'GET' || !request.url?.startsWith('/api/v1/employees/123?')) { response.writeHead(404).end(); return; }
    if (request.headers.authorization !== `Basic ${Buffer.from('0123456789abcdef0123456789abcdef01234567:x').toString('base64')}`) { response.writeHead(401).end(); return; }
    if (request.url.includes('REJECT')) { response.writeHead(403).end('{}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ id: '123', firstName: 'Ava', workEmail: 'ava@example.com' }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('BambooHR fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-bamboohr-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new BambooHRClient({ companyDomain: 'acme' }, {
    resolve: async () => ({ apiKey: new SecretString('0123456789abcdef' + '0123456789abcdef01234567') }),
  }, { apiBaseURL: `http://127.0.0.1:${address.port}/api/v1` });
  const flow = new GetEmployeeFlow(newConnection(provider, { provider: 'bamboohr', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `bamboohr-get-${randomUUID()}`;
    await client.startFlow(flow, successId, { employeeId: '123', fields: ['firstName', 'workEmail'] });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<EmployeeRecord>().fields?.firstName).toBe('Ava');
    const rejectionId = `bamboohr-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { employeeId: '123', fields: ['REJECT'] });
    const rejected = await client.waitForFlow(rejectionId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
