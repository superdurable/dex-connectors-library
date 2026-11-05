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
import { NetlifyClient } from '../../src/client.js';
import { newConnection } from '../../src/generated/connection.js';
import type { SitesPage } from '../../src/generated/models.js';
import { NetlifyLookupFlow } from '../../examples/netlify-lookup/flow/workflow.js';

it('executes the checked-in lookup Flow through a real Dex Worker and Client', async () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  let requests = 0;
  const server = createServer((request, response) => {
    if (request.method !== 'GET' || request.url !== '/sites' || request.headers.authorization !== 'Bearer test-accesstoken') {
      response.writeHead(400).end();
      return;
    }
    requests++;
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end('[{"id":"site-meridian","name":"meridian-web","url":"https://meridian.netlify.app","admin_url":"https://app.netlify.com/sites/meridian-web"}]');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const providerAddress = server.address();
  if (!providerAddress || typeof providerAddress === 'string') throw new Error('provider port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-netlify-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new NetlifyClient({ baseUrl: `http://127.0.0.1:${providerAddress.port}` }, {
    resolve: async () => ({ accessToken: new SecretString('test-accesstoken') }),
  });
  const flow = new NetlifyLookupFlow(newConnection(provider, { provider: 'netlify', name: 'netlify' }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: address });
  const client = new Client(registry, cache, { serverAddress: address });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `netlify-lookup-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<SitesPage>().sites?.[0]?.id).toBe('site-meridian');
    expect(requests).toBe(1);
  } finally {
    await client.close();
    await worker.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
