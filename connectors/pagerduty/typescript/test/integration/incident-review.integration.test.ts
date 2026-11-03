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
import { PagerDutyClient, newConnection, type Incident, type IncidentPage } from '../../src/index.js';
import { ConnectionName, IncidentLookupFlow, IncidentReviewFlow } from '../../examples/incident-review/flow/workflow.js';

it('lists incidents through real Dex and fails an unwired provider rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  let hasRateLimited = false;
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Token token=test-secret-token') { response.writeHead(401).end(); return; }
    if (request.url?.includes('offset=2')) { response.writeHead(403).end(); return; }
    if (request.url?.includes('offset=3') && !hasRateLimited) { hasRateLimited = true; response.writeHead(429).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    if (request.url === '/incidents/P123456') { response.end(JSON.stringify({ incident: { id: 'P123456', title: 'API latency', status: 'triggered' } })); return; }
    response.end(JSON.stringify({ incidents: [{ id: 'P123456', title: 'API latency', status: 'triggered' }], limit: 25, offset: 1, more: false }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('PagerDuty fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-pagerduty-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new PagerDutyClient({ baseUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => ({ apiKey: new SecretString('test-secret-token') }),
  });
  const connection = newConnection(provider, { provider: 'pagerduty', name: ConnectionName });
  const flow = new IncidentReviewFlow(connection);
  const lookupFlow = new IncidentLookupFlow(connection);
  const registry = new Registry([flow, lookupFlow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `pagerduty-list-${randomUUID()}`;
    await client.startFlow(flow, successId, { limit: 25, offset: 1 });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<IncidentPage>().incidents?.[0]?.id).toBe('P123456');
    const lookupId = `pagerduty-lookup-${randomUUID()}`;
    await client.startFlow(lookupFlow, lookupId, { id: 'P123456' });
    const lookedUp = await client.waitForFlow(lookupId, 60_000);
    expect(lookedUp.status, lookedUp.errorMessage).toBe('completed');
    expect(lookedUp.singleOutput<Incident>().title).toBe('API latency');
    const rejectedId = `pagerduty-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { limit: 25, offset: 2 });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    const retryId = `pagerduty-retry-${randomUUID()}`;
    await client.startFlow(flow, retryId, { limit: 25, offset: 3 });
    const retried = await client.waitForFlow(retryId, 60_000);
    expect(retried.status, retried.errorMessage).toBe('completed');
    expect(requests).toHaveLength(5);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
