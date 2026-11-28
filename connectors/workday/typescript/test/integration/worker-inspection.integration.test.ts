// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { expect, it, vi } from 'vitest';
import { newProjectConnection, type WorkerPage, type WorkerSummary } from '../../src/index.js';
import { WorkerListFlow } from '../../examples/worker-inspection/list-flow.js';
import { ConnectionName, WorkerFlow } from '../../examples/worker-inspection/flow.js';

it('retries a provider 503 and persists a worker through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  let exchanges = 0;
  const server = createServer(async (request, response) => {
    if (request.url === '/token') {
      exchanges++;
      const chunks: Buffer[] = [];
      for await (const chunk of request) chunks.push(Buffer.from(chunk));
      const form = new URLSearchParams(Buffer.concat(chunks).toString());
      if (request.method !== 'POST' || form.get('grant_type') !== 'refresh_token' || form.get('refresh_token') !== 'synthetic-refresh'
          || request.headers.authorization !== 'Basic ' + Buffer.from('synthetic-client:synthetic-secret').toString('base64')) {
        response.writeHead(401).end();
        return;
      }
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({
        access_token: 'synthetic-workday-token', token_type: 'Bearer', expires_in: 3600,
      }));
      return;
    }
    if (request.headers.authorization !== 'Bearer synthetic-workday-token') { response.writeHead(401).end(); return; }
    const requestURL = new URL(request.url ?? '', 'http://127.0.0.1');
    if (requestURL.pathname === '/api/common/v1/demo/workers') {
      if (requestURL.searchParams.get('search') === 'reflected') {
        attempts++;
        response.writeHead(200).end(String.raw`{"data":[{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","descriptor":"\u0073ynthetic-workday-token"}]}`);
        return;
      }
      if (request.method !== 'GET' || requestURL.searchParams.get('limit') !== '2'
          || requestURL.searchParams.get('offset') !== '5' || requestURL.searchParams.get('search') !== ' Li+ ') {
        response.writeHead(400).end();
        return;
      }
      attempts++;
      if (attempts === 3) { response.writeHead(503).end(); return; }
      response.writeHead(200).end(JSON.stringify({
        total: 10, data: [{ id: 'a'.repeat(32), descriptor: 'Li', privateField: 'excluded' }],
      }));
      return;
    }
    if (requestURL.pathname !== '/api/common/v1/demo/workers/0123456789abcdef0123456789abcdef') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ id: '0123456789abcdef0123456789abcdef', descriptor: 'Ada Lovelace' }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Worker fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-workday-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const baseUrl = `http://127.0.0.1:${address.port}`;
  const path = join(directory, 'connections.json');
  await writeFile(path, JSON.stringify({
    schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
    connections: [{
      connectorId: 'workday', connectionName: ConnectionName, provider: 'workday',
      modulePath: 'github.com/superdurable/dex-connectors-library/connectors/workday/go',
      configuration: {
        baseUrl: baseUrl + '/api/common/v1/demo', authorizationEndpoint: baseUrl + '/authorize',
        tokenEndpoint: 'https://tenant.example.com/token', scopes: ['Staffing'], timeout: 1732000000,
      },
      credentials: {
        oauth_client_id: 'synthetic-client', oauth_client_secret: 'synthetic-secret',
        access_token: 'expired', refresh_token: 'synthetic-refresh',
      },
      credentialExpiresAt: '2000-01-01T00:00:00Z',
    }],
  }), { mode: 0o600 });
  const send = globalThis.fetch;
  const tokenFetch = vi.spyOn(globalThis, 'fetch').mockImplementation((url, init) => {
    if (String(url) === 'https://tenant.example.com/token') return send(baseUrl + '/token', init);
    return send(url, init);
  });
  const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
  const connection = newProjectConnection(project, ConnectionName, {
    send: request => {
      expect(request.timeoutMs).toBe(1732);
      return sendProviderRequest(request);
    },
  });
  const flow = new WorkerFlow(connection);
  const pageFlow = new WorkerListFlow(connection);
  const registry = new Registry([flow, pageFlow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `workday-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { workerId: '0123456789abcdef0123456789abcdef' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<WorkerSummary>()).toEqual({ id: '0123456789abcdef0123456789abcdef', descriptor: 'Ada Lovelace' });
    expect(attempts).toBe(2);
    const pageId = `workday-typescript-page-${randomUUID()}`;
    await client.startFlow(pageFlow, pageId, { limit: 2, offset: 5, search: ' Li+ ' });
    const page = await client.waitForFlow(pageId, 60_000);
    expect(page.status, page.errorMessage).toBe('completed');
    expect(page.singleOutput<WorkerPage>()).toEqual({ total: 10, data: [{ id: 'a'.repeat(32), descriptor: 'Li' }] });
    expect(attempts).toBe(4);
    const reflectedId = `workday-reflection-${randomUUID()}`;
    await client.startFlow(pageFlow, reflectedId, { search: 'reflected' });
    const rejected = await client.waitForFlow(reflectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('defect');
    expect(rejected.errorMessage).not.toContain('synthetic-workday-token');
    expect(attempts).toBe(5);
    expect(exchanges).toBe(1);
  } finally {
    tokenFetch.mockRestore();
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
