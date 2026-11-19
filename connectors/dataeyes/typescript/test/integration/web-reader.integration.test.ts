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
import { DataEyesClient, newConnection, type ReadWebPageOutput } from '../../src/index.js';
import { DataEyesWebPageFlow, ConnectionName, webPageAttribute } from '../../examples/web-reader/flow.js';

it('retries a 503 through a real Dex Worker and persists the public page', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    if (request.method !== 'POST' || request.url !== '/v1/reader' || request.headers.authorization !== 'Bearer test-secret') { response.writeHead(401).end(); return; }
    const chunks: Buffer[] = [];
    request.on('data', chunk => chunks.push(Buffer.from(chunk)));
    request.on('end', () => {
      if (Buffer.concat(chunks).toString() !== JSON.stringify({ url: 'https://example.com', format: 'markdown' })) { response.writeHead(400).end(); return; }
      requests++;
      if (requests === 1) { response.writeHead(503, { 'Retry-After': '1' }).end(); return; }
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ code: 0, data: { metadata: { title: 'Example Domain' }, markdown: '# Example Domain' } }));
    });
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-dataeyes-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new DataEyesClient({}, { resolve: async () => ({ apiKey: new SecretString('test-secret') }) }, { readerEndpoint: `http://127.0.0.1:${address.port}/v1/reader` });
  const flow = new DataEyesWebPageFlow(newConnection(provider, { provider: 'dataeyes', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `dataeyes-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { url: 'https://example.com' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<ReadWebPageOutput>()).toEqual({ title: 'Example Domain', markdown: '# Example Domain' });
    const [persistedPage] = await client.getAttributes(flowId, webPageAttribute);
    expect(persistedPage).toEqual({ title: 'Example Domain', markdown: '# Example Domain' });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
