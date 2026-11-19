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
import { SendGridClient } from '../../src/client.js';
import { newConnection, type StatsOutput } from '../../src/index.js';
import { ConnectionName, GetStatsFlow } from '../../examples/stats/flow/workflow.js';

it('reads statistics through real Dex and fails an unwired provider rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer test-key-123') { response.writeHead(401).end(); return; }
    const url = new URL(request.url ?? '/', 'http://localhost');
    if (request.method !== 'GET' || url.pathname !== '/v3/stats') { response.writeHead(404).end(); return; }
    if (url.searchParams.get('start_date') === '2026-10-03') { response.writeHead(403).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end('[{"date":"2026-10-01","stats":[{"metrics":{"requests":8,"delivered":7}}]}]');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('SendGrid fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-sendgrid-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new SendGridClient({ baseUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => ({ apiKey: new SecretString('test-key-123') }),
  });
  const flow = new GetStatsFlow(newConnection(provider, { provider: 'sendgrid', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `sendgrid-stats-${randomUUID()}`;
    await client.startFlow(flow, successId, { startDate: '2026-10-01', endDate: '2026-10-02' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<StatsOutput>().days?.[0]?.delivered).toBe(7);
    const rejectionId = `sendgrid-stats-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { startDate: '2026-10-03' });
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
