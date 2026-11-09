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
import { MicrosoftAzureBlobClient } from '../../src/client.js';
import { newConnection, type BlobDownload, type Credentials } from '../../src/index.js';
import { ConnectionName, BlobDownloadFlow } from '../../examples/blob-download/flow.js';

it('retries a provider 503 and persists the blob download through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (!request.headers.authorization?.startsWith('SharedKey exampleacct:')) { response.writeHead(401).end(); return; }
    if (request.method !== 'GET' || request.url !== '/docs/example.txt' || request.headers['x-ms-version'] !== '2023-11-03') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'text/plain', ETag: '"0x1"' }).end('hello');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Azure Blob Storage fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-azureblob-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { accountKey: new SecretString('c2VjcmV0LXRlc3Qta2V5') };
  const provider = new MicrosoftAzureBlobClient({ endpoint: `http://127.0.0.1:${address.port}`, accountName: 'exampleacct' }, {
    resolve: async () => credentials,
  });
  const connection = newConnection(provider, { provider: 'microsoft-azure-blob', name: ConnectionName });
  const flow = new BlobDownloadFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `azureblob-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { container: 'docs', blobName: 'example.txt' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<BlobDownload>().dataBase64).toBe('aGVsbG8=');
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
