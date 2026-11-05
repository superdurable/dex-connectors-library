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
import { WebhookClient } from '../../src/client.js';
import { newConnection, type SendEventOutput } from '../../src/index.js';
import { ConnectionName, SendEventFlow } from '../../examples/send-event/flow/workflow.js';

it('delivers through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: Array<{ id: string; signature: string; body: string }> = [];
  const server = createServer(async (request, response) => {
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    const body = Buffer.concat(chunks).toString('utf8');
    requests.push({ id: String(request.headers['webhook-id'] ?? ''), signature: String(request.headers['webhook-signature'] ?? ''), body });
    if (JSON.parse(body).type === 'reject') response.writeHead(422).end();
    else response.writeHead(202).end();
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('receiver address unavailable');
  const receiverUrl = `http://127.0.0.1:${address.port}/events`;
  const directory = await mkdtemp(join(tmpdir(), 'dex-webhook-send-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new WebhookClient({ deliveryUrl: 'https://receiver.example/events' }, {
    resolve: async () => ({ signingSecret: new SecretString('SENTINEL-SIGNING-SECRET') }),
  }, { send: async request => {
    const result = await fetch(receiverUrl, { method: request.method, headers: request.headers ?? {}, body: Buffer.from(request.body ?? []) });
    return { status: result.status, headers: { 'retry-after': result.headers.get('retry-after') ?? '' }, body: new Uint8Array(await result.arrayBuffer()) };
  } });
  const flow = new SendEventFlow(newConnection(provider, { provider: 'webhook', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `webhook-send-${randomUUID()}`;
    await client.startFlow(flow, successId, { payload: { type: 'accepted', submissionId: 'evt_123' } });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<SendEventOutput>().statusCode).toBe(202);
    const rejectionId = `webhook-send-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { payload: { type: 'reject', submissionId: 'evt_456' } });
    const rejected = await client.waitForFlow(rejectionId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('rejected');
    expect(requests).toHaveLength(2);
    expect(requests[0]?.id).toMatch(/^msg_[0-9a-f]{32}$/);
    expect(requests[0]?.signature).toMatch(/^v1,/);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
