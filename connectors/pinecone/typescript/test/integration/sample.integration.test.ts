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
import { PineconeClient } from '../../src/client.js';
import { newConnection, type UpsertVectorsOutput } from '../../src/index.js';
import { ConnectionName, PineconeSampleFlow } from '../../examples/sample-pinecone/flow/workflow.js';

it('upserts and fails an unwired rejection through a real Dex Worker and Client', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const requestBodies: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.method !== 'POST' || request.url !== '/vectors/upsert') { response.writeHead(404).end(); return; }
    if (request.headers['api-key'] !== 'pinecone-test-key') { response.writeHead(401).end(); return; }
    const chunks: Buffer[] = [];
    request.on('data', (chunk: Buffer) => chunks.push(chunk));
    request.on('end', () => {
      const requestBody = Buffer.concat(chunks).toString('utf8');
      requestBodies.push(requestBody);
      const body = JSON.parse(requestBody) as { vectors: Array<{ id: string }> };
      if (body.vectors[0]?.id === 'reject') { response.writeHead(403, { 'Content-Type': 'application/json' }).end('{}'); return; }
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ upsertedCount: body.vectors.length }));
    });
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Pinecone fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-pinecone-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new PineconeClient({ baseUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => ({ apiKey: new SecretString('pinecone-test-key') }),
  });
  const flow = new PineconeSampleFlow(newConnection(provider, { provider: 'pinecone', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `pinecone-upsert-${randomUUID()}`;
    await client.startFlow(flow, successId, { namespace: 'example', vectors: [{ id: 'vec-meridian', values: [0.123456789, 16777217, 0.30000001192092896] }] });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<UpsertVectorsOutput>().upsertedCount).toBe(1);
    expect(requestBodies[0]).toContain('"values":[0.12345679,16777216,0.3]');
    const rejectionId = `pinecone-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { vectors: [{ id: 'reject', values: [1, 2] }] });
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
