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
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { expect, it } from 'vitest';
import { ApifoxClient } from '../../src/client.js';
import { newConnection, type GetProjectAPISummaryOutput } from '../../src/index.js';
import { ConnectionName, ProjectAPISummaryFlow } from '../../examples/project-summary/flow.js';

it('retries Apifox 503 through a real Dex Worker and persists the bounded summary', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'POST' || request.url !== '/v1/projects/123/export-openapi' || request.headers.authorization !== 'Bearer test-secret' || request.headers['x-apifox-api-version'] !== '2024-03-28') {
      response.writeHead(400).end(); return;
    }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({
      openapi: '3.1.0', info: { title: 'Example API', version: '1.0.0' },
      paths: { '/items': { get: { summary: 'List' }, post: { summary: 'Create' } } },
    }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Apifox fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-apifox-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new ApifoxClient({}, { resolve: async () => ({ accessToken: new SecretString('test-secret') }) }, {
    send: request => sendProviderRequest({
      ...request, url: `http://127.0.0.1:${address.port}${new URL(request.url).pathname}`,
      addressPolicy: 'privateNetworkHTTP',
    }),
  });
  const flow = new ProjectAPISummaryFlow(newConnection(provider, { provider: 'apifox', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `apifox-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { projectID: '123' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<GetProjectAPISummaryOutput>()).toEqual({ title: 'Example API', version: '1.0.0', endpointCount: 2 });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
