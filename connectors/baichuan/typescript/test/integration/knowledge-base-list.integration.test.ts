// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { SecretString } from '@superdurable/connectors-sdk';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { ConnectionName, KnowledgeBaseListFlow } from '../../examples/knowledge-base-list/flow.js';
import { BaichuanClient } from '../../src/client.js';
import { newConnection, type ListKnowledgeBasesOutput } from '../../src/index.js';

it('retries Baichuan 503 through a real Dex Worker and persists only knowledge-base metadata', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'GET' || request.url !== '/v1/kbs' || request.headers.authorization !== 'Bearer test-baichuan-key-123' || request.headers.accept !== 'application/json') {
      response.writeHead(400).end(); return;
    }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ object: 'list', data: [{ id: 'kb-abc123', object: 'knowledge-base', name: 'sample', description: 'Example', status: 'online', file_ids: ['file-123'], created_at: 1701828706, updated_at: 1701828706, private: 'secret' }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Baichuan fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-baichuan-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new BaichuanClient({ baseUrl: `http://127.0.0.1:${address.port}` }, { resolve: async () => ({ apiKey: new SecretString('test-baichuan-key-123') }) });
  const flow = new KnowledgeBaseListFlow(newConnection(provider, { provider: 'baichuan', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `baichuan-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<ListKnowledgeBasesOutput>()).toEqual({ knowledgeBases: [{ id: 'kb-abc123', name: 'sample', description: 'Example', status: 'online', fileIds: ['file-123'], createdAt: 1701828706, updatedAt: 1701828706 }] });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
