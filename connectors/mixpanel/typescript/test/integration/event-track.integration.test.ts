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
import { MixpanelClient, newConnection, type EventImportResult } from '../../src/index.js';
import { ConnectionName, EventTrackFlow } from '../../examples/event-track/flow/workflow.js';

it('imports through real Dex and fails an unwired provider rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const insertIds: string[] = [];
  const server = createServer((request, response) => {
    if (request.method !== 'POST' || request.url !== '/import?project_id=12345&strict=1') { response.writeHead(404).end(); return; }
    if (request.headers.authorization !== `Basic ${Buffer.from('service-user:service-secret').toString('base64')}`) { response.writeHead(401).end(); return; }
    const chunks: Buffer[] = [];
    request.on('data', (chunk: Buffer) => chunks.push(chunk));
    request.on('end', () => {
      const event = (JSON.parse(Buffer.concat(chunks).toString('utf8')) as Array<{ event: string; properties: Record<string, unknown> }>)[0];
      if (!event) { response.writeHead(400).end(); return; }
      insertIds.push(String(event.properties.$insert_id));
      if (event.event === 'Rejected') { response.writeHead(401).end(); return; }
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end('{"code":200,"num_records_imported":1,"status":"OK"}');
    });
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Mixpanel fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-mixpanel-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new MixpanelClient({ baseUrl: `http://127.0.0.1:${address.port}`, exportBaseUrl: `http://127.0.0.1:${address.port}/api/2.0`, projectId: 12345 }, {
    resolve: async () => ({ serviceAccountUsername: new SecretString('service-user'), serviceAccountSecret: new SecretString('service-secret') }),
  });
  const flow = new EventTrackFlow(newConnection(provider, { provider: 'mixpanel', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const input = { event: 'Signed Up', distinctID: 'member-1', time: 1793232000, properties: { source: 'landing' } };
    const successId = `mixpanel-event-${randomUUID()}`;
    await client.startFlow(flow, successId, input);
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<EventImportResult>().imported).toBe(true);
    const rejectionId = `mixpanel-event-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { ...input, event: 'Rejected' });
    const rejected = await client.waitForFlow(rejectionId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(insertIds).toHaveLength(2);
    expect(insertIds.every(id => id.length === 36)).toBe(true);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
