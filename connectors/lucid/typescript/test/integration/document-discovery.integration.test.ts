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
import { LucidClient } from '../../src/client.js';
import { newConnection, type DocumentsPage, type Credentials } from '../../src/index.js';
import { ConnectionName, DocumentDiscoveryFlow } from '../../examples/document-discovery/flow.js';

it('retries a provider 503 and persists the document page through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const pageToken = 'cursor-1';
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer sample-lucid-key' || request.headers['lucid-api-version'] !== '1') { response.writeHead(401).end(); return; }
    if (request.method !== 'POST' || !request.url?.startsWith('/v1/documents/search?') || !request.url.includes('pageToken=cursor-1')) { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify([{
      documentId: '110808fd-4553-4316-bccf-4f25ff59a532', title: 'Example document',
    }]));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Lucid fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-lucid-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { apiKey: new SecretString('sample-lucid-key') };
  const provider = new LucidClient({ baseUrl: `http://127.0.0.1:${address.port}/v1` }, {
    resolve: async () => credentials,
  });
  const connection = newConnection(provider, { provider: 'lucid', name: ConnectionName });
  const flow = new DocumentDiscoveryFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `lucid-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { pageToken });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<DocumentsPage>().documents?.[0]?.id).toBe('110808fd-4553-4316-bccf-4f25ff59a532');
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
