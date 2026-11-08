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
import { AzureDevOpsClient } from '../../src/client.js';
import { newConnection, type Projects } from '../../src/index.js';
import { ConnectionName, ProjectDiscoveryFlow } from '../../examples/project-discovery/flow.js';

it('retries a provider 503 and persists projects through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Basic OnN5bnRoZXRpYy1hcGkta2V5') { response.writeHead(401).end(); return; }
    if (request.url !== '/example/_apis/projects?%24top=20&%24skip=0&api-version=7.1') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ count: 1, value: [{ id: 'eb6e4656-77fc-42a1-9181-4c6d8e9da5d1', name: 'Example', state: 'wellFormed' }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('AzureDevOps fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-azure-devops-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new AzureDevOpsClient({ baseUrl: `http://127.0.0.1:${address.port}/example` }, {
    resolve: async () => ({ personalAccessToken: new SecretString('synthetic-api-key') }),
  });
  const connection = newConnection(provider, { provider: 'azure-devops', name: ConnectionName });
  const flow = new ProjectDiscoveryFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `azure-devops-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { top: 20, skip: 0 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<Projects>().projects?.[0]?.name).toBe('Example');
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
