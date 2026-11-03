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
import { CohereClient } from '../../src/client.js';
import { newConnection } from '../../src/index.js';
import { CohereSearchFlow, ConnectionName, type SearchOutcome } from '../../examples/cohere-search/workflow.js';

it('embeds, reranks, retries, and fails an unwired rejection through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  let retryAttempts = 0;
  const server = createServer((request, response) => {
    const chunks: Buffer[] = [];
    request.on('data', (chunk: Buffer) => chunks.push(chunk));
    request.on('end', () => {
      const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { texts?: string[]; query?: string };
      requests.push(`${request.method} ${request.url}`);
      if (request.headers.authorization !== 'Bearer test-secret-key') { response.writeHead(401).end(); return; }
      if (request.url === '/v2/embed') {
        if (body.texts?.[0] === 'reject') { response.writeHead(401).end('{"message":"SENTINEL"}'); return; }
        if (body.texts?.[0] === 'retry' && retryAttempts++ === 0) { response.writeHead(503).end(); return; }
        response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ id: 'embed_1', embeddings: { float: [[0.1, 0.2]] } }));
        return;
      }
      if (request.url === '/v2/rerank') {
        response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ results: [{ index: 1, relevance_score: 0.9 }, { index: 0, relevance_score: 0.1 }] }));
        return;
      }
      response.writeHead(404).end();
    });
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Cohere fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-cohere-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new CohereClient({ baseUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => ({ apiKey: new SecretString('test-secret-key') }),
  });
  const flow = new CohereSearchFlow(newConnection(provider, { provider: 'cohere', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    for (const query of ['hello', 'retry']) {
      const flowID = `cohere-${query}-${randomUUID()}`;
      await client.startFlow(flow, flowID, { query, documents: ['weather', 'release note'] });
      const result = await client.waitForFlow(flowID, 60_000);
      expect(result.status, result.errorMessage).toBe('completed');
      expect(result.singleOutput<SearchOutcome>().results.map(item => item.index)).toEqual([1, 0]);
    }
    const rejectedID = `cohere-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedID, { query: 'reject', documents: ['weather', 'release note'] });
    const rejected = await client.waitForFlow(rejectedID, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(rejected.errorMessage).not.toContain('SENTINEL');
    expect(requests.filter(value => value === 'POST /v2/embed')).toHaveLength(4);
    expect(requests.filter(value => value === 'POST /v2/rerank')).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
