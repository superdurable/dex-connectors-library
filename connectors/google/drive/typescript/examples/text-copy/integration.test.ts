// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { DriveClient, newConnection, type Credentials, type File } from '../../src/index.js';
import { ConnectionName, TextCopyFlow } from './flow.js';

it('copies text through four provider operations on a real Dex Worker', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    const url = new URL(request.url ?? '/', 'http://localhost');
    requests.push(`${request.method} ${url.pathname}`);
    if (request.headers.authorization !== 'Bearer drive-token') { response.writeHead(401).end(); return; }
    response.setHeader('X-Goog-Request-Id', 'drive-fixture-request');
    response.setHeader('Content-Type', 'application/json');
    if (request.method === 'GET' && url.pathname === '/drive/v3/files') {
      const isLookup = (url.searchParams.get('q') ?? '').startsWith('appProperties');
      const isMissing = (url.searchParams.get('q') ?? '').includes('Missing');
      response.writeHead(200).end(JSON.stringify({ files: isLookup || isMissing ? [] : [{ id: 'source_1', name: 'Ops Policy', mimeType: 'application/vnd.google-apps.document' }] }));
      return;
    }
    if (request.method === 'GET' && url.pathname === '/drive/v3/files/source_1') { response.writeHead(200).end(JSON.stringify({ id: 'source_1', name: 'Ops Policy', mimeType: 'application/vnd.google-apps.document' })); return; }
    if (request.method === 'GET' && url.pathname === '/drive/v3/files/source_1/export') { response.writeHead(200).end('Policy text'); return; }
    if (request.method === 'POST' && url.pathname === '/upload/drive/v3/files') { response.writeHead(200).end(JSON.stringify({ id: 'copy_1', name: 'Ops Policy copy.txt', mimeType: 'text/plain', size: '11' })); return; }
    if (request.method === 'GET' && url.pathname === '/drive/v3/files/copy_1') { response.writeHead(200).end(JSON.stringify({ id: 'copy_1', name: 'Ops Policy copy.txt', mimeType: 'text/plain', size: '11' })); return; }
    response.writeHead(404).end('{}');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('fixture address is unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-drive-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const reference = { provider: 'google', name: ConnectionName };
  const credentials: Credentials = { accessToken: new SecretString('drive-token') };
  const provider = new DriveClient({ endpoint: `http://127.0.0.1:${address.port}` }, new StaticCredentialProvider([[reference, credentials]]),
    { refreshDriver: { refreshRequired: () => false, refresh: async () => { throw new Error('refresh was not expected'); } } });
  const flow = new TextCopyFlow(newConnection(provider, reference));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `drive-copy-${randomUUID()}`;
    await client.startFlow(flow, successId, { sourceName: 'Ops Policy', copyName: 'Ops Policy copy.txt' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<File>().id).toBe('copy_1');
    const missingId = `drive-missing-${randomUUID()}`;
    await client.startFlow(flow, missingId, { sourceName: 'Missing', copyName: 'No copy' });
    const missing = await client.waitForFlow(missingId, 60_000);
    expect(missing.status).toBe('failed');
    expect(missing.errorMessage).toContain('notFound');
    expect(requests).toEqual([
      'GET /drive/v3/files', 'GET /drive/v3/files/source_1', 'GET /drive/v3/files/source_1/export',
      'GET /drive/v3/files', 'POST /upload/drive/v3/files', 'GET /drive/v3/files/copy_1', 'GET /drive/v3/files',
    ]);
  } finally {
    await client.close(); await worker.close(); cache.close(); await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
