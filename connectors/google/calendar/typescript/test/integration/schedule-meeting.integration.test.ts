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
import { expect, it } from 'vitest';
import { CalendarClient } from '../../src/client.js';
import { newConnection, type Event } from '../../src/index.js';
import { ConnectionName, ScheduleMeetingFlow } from '../../examples/schedule-meeting/flow/workflow.js';

it('reads an event through a real Dex Worker and fails an unwired provider rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer calendar-token') { response.writeHead(401).end(); return; }
    if (request.url?.includes('/denied12345')) { response.writeHead(403).end('{}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ id: 'meetinghold0001', status: 'confirmed', summary: 'Meridian kickoff', start: { dateTime: '2026-02-24T09:00:00-08:00', timeZone: 'America/Los_Angeles' }, end: { dateTime: '2026-02-24T10:00:00-08:00', timeZone: 'America/Los_Angeles' } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Calendar stand-in port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-calendar-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const reference = { provider: 'google', name: ConnectionName };
  const provider = new CalendarClient({ endpoint: `http://127.0.0.1:${address.port}/calendar/v3` }, new StaticCredentialProvider([[reference, { accessToken: new SecretString('calendar-token') }]]));
  const flow = new ScheduleMeetingFlow(newConnection(provider, reference));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `calendar-event-${randomUUID()}`;
    await client.startFlow(flow, successId, { calendarId: 'primary', eventId: 'meetinghold0001' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<Event>().summary).toBe('Meridian kickoff');
    const rejectedId = `calendar-denied-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { calendarId: 'primary', eventId: 'denied12345' });
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
