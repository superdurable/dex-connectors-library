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
import { PandaDocClient } from '../../src/client.js';
import { newConnection, type DocumentStatus, type Credentials } from '../../src/index.js';
import { ConnectionName, DocumentInspectionFlow } from '../../examples/document-inspection/flow.js';

it('retries a provider 503 and persists the document through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const documentId = 'fANp5wW5EwY7AkiTkTGekZ';
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'API-Key sample-pandadoc-api-key') { response.writeHead(401).end(); return; }
    if (request.url !== `/public/v1/documents/${encodeURIComponent(documentId)}`) { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({
      id: documentId, name: 'Example agreement', status: 'document.completed',
    }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('PandaDoc fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-pandadoc-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { apiKey: new SecretString('sample-pandadoc-api-key') };
  const provider = new PandaDocClient({ baseUrl: `http://127.0.0.1:${address.port}/public/v1` }, {
    resolve: async () => credentials,
  });
  const connection = newConnection(provider, { provider: 'pandadoc', name: ConnectionName });
  const flow = new DocumentInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `pandadoc-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { documentId });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<DocumentStatus>().id).toBe(documentId);
    expect(result.singleOutput<DocumentStatus>().status).toBe('document.completed');
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
