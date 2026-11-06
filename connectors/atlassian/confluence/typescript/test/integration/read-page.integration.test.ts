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
import { ConfluenceClient } from '../../src/client.js';
import { newConnection } from '../../src/index.js';
import { ConnectionName, ReadConfluencePageFlow, type PageBrief } from '../../examples/read-page/flow/workflow.js';

const cloudId = '11223344-a1b2-4b33-8c44-def123456789';
const contentPath = `/ex/confluence/${cloudId}/wiki/api/v2/pages/600002?body-format=storage`;

it('reads a page through real Dex and fails an unwired provider rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.method !== 'GET' || request.url !== contentPath || request.headers.authorization !== 'Bearer confluence-test-access') { response.writeHead(404).end(); return; }
    if (request.headers['x-reject-test'] === 'yes' || requests.length === 2) { response.writeHead(403, { 'Content-Type': 'application/json' }).end('{"error":"forbidden"}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json', 'Atl-Traceid': 'trace-1' });
    response.end(JSON.stringify({
      id: '600002', title: 'Remote work policy', status: 'current', spaceId: '98306',
      createdAt: '2026-10-25T09:00:00Z', version: { number: 2, createdAt: '2026-10-25T09:00:00Z' },
      body: { storage: { representation: 'storage', value: '<h1>Remote work</h1><p>Hello</p>' } },
      _links: { base: 'https://ops.atlassian.net/wiki', webui: '/spaces/OPS/pages/600002' },
    }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('provider fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-confluence-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const connection = { provider: 'atlassian', name: ConnectionName };
  const credentials = new StaticCredentialProvider([[connection, {
    oauthClientId: 'test-client', oauthClientSecret: new SecretString('test-secret'),
    accessToken: new SecretString('confluence-test-access'), refreshToken: new SecretString('test-refresh'),
  }]]);
  const provider = new ConfluenceClient({ cloudId, endpoint: `http://127.0.0.1:${address.port}` }, credentials);
  const flow = new ReadConfluencePageFlow(newConnection(provider, connection));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const input = { pageId: '600002', bodyRepresentation: 'storage' as const, bodyFormat: 'markdown' as const, maxBodyCharacters: 200 };
    const successId = `confluence-read-${randomUUID()}`;
    await client.startFlow(flow, successId, input);
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<PageBrief>()).toEqual({ pageId: '600002', title: 'Remote work policy', versionNumber: 2 });
    const rejectionId = `confluence-read-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, input);
    const rejected = await client.waitForFlow(rejectionId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toEqual([`GET ${contentPath}`, `GET ${contentPath}`]);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
