// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { expect, it } from 'vitest';
import { CalendlyClient } from '../../src/client.js';
import { newConnection, type ScheduledEvent } from '../../src/index.js';
import { ConnectionName, ScheduledEventFlow } from '../../examples/scheduled-event/flow/workflow.js';

it('reads a Calendly event through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer stand-in-token') { response.writeHead(401).end(); return; }
    if (request.url?.endsWith('/DENIED001')) { response.writeHead(403, { 'Content-Type': 'application/json' }).end('{}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ resource: { uri: 'https://api.calendly.com/scheduled_events/EVT00001', name: 'Discovery Call', status: 'active', start_time: '2026-11-02T17:00:00Z', end_time: '2026-11-02T17:30:00Z' } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Calendly stand-in port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-calendly-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const connection = { provider: 'calendly', name: ConnectionName };
  const provider = new CalendlyClient({}, new StaticCredentialProvider([[connection, { authMethodId: 'personal-access-token', accessToken: new SecretString('stand-in-token') }]]), {
    send: request => sendProviderRequest({ ...request, url: request.url.replace('https://api.calendly.com', `http://127.0.0.1:${address.port}`), addressPolicy: 'privateNetworkHTTP' }),
  });
  const flow = new ScheduledEventFlow(newConnection(provider, connection));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `calendly-event-${randomUUID()}`;
    await client.startFlow(flow, successId, { scheduledEventUri: 'https://api.calendly.com/scheduled_events/EVT00001' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<ScheduledEvent>().name).toBe('Discovery Call');
    const rejectedId = `calendly-denied-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { scheduledEventUri: 'https://api.calendly.com/scheduled_events/DENIED001' });
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
