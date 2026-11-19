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
import { DedaoClient, newConnection, type GetNoteTaskStatusOutput } from '../../src/index.js';
import { DedaoTaskStatusFlow, ConnectionName, taskStatusAttribute } from '../../examples/task-status/flow.js';

it('retries a 503 through a real Dex Worker and persists only task metadata', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    if (request.method !== 'POST' || request.url !== '/open/api/v1/resource/note/task/progress' || request.headers.authorization !== 'gk_live_test-secret' || request.headers['x-client-id'] !== 'cli_test-client') { response.writeHead(401).end(); return; }
    const chunks: Buffer[] = [];
    request.on('data', chunk => chunks.push(Buffer.from(chunk)));
    request.on('end', () => {
      if (Buffer.concat(chunks).toString() !== '{"task_id":"69c3995e99f5a67e"}') { response.writeHead(400).end(); return; }
      requests++;
      if (requests === 1) { response.writeHead(503, { 'Retry-After': '1' }).end(); return; }
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ success: true, data: { status: 'success', note_id: '1907975006118548200', content: 'private note' } }));
    });
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-dedao-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new DedaoClient({}, { resolve: async () => ({ apiKey: new SecretString('gk_live_test-secret'), clientID: new SecretString('cli_test-client') }) }, {
    send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}/open/api/v1/resource/note/task/progress`, addressPolicy: 'privateNetworkHTTP' }),
  });
  const flow = new DedaoTaskStatusFlow(newConnection(provider, { provider: 'dedao', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `dedao-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { taskID: '69c3995e99f5a67e' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const expected: GetNoteTaskStatusOutput = { taskID: '69c3995e99f5a67e', status: 'success', noteID: '1907975006118548200' };
    expect(result.singleOutput<GetNoteTaskStatusOutput>()).toEqual(expected);
    const [persistedStatus] = await client.getAttributes(flowId, taskStatusAttribute);
    expect(persistedStatus).toEqual(expected);
    expect(JSON.stringify(result.singleOutput<GetNoteTaskStatusOutput>())).not.toContain('private note');
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
