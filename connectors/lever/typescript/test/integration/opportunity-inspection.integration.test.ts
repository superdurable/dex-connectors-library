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
import { LeverClient } from '../../src/client.js';
import { newConnection, type OpportunityStage, type Credentials } from '../../src/index.js';
import { ConnectionName, OpportunityInspectionFlow } from '../../examples/opportunity-inspection/flow.js';

it('retries a provider 503 and persists the opportunity through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const opportunityId = '250d8f03-738a-4bba-a671-8a3d73477145';
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Basic c2FtcGxlLWxldmVyLWFwaS1rZXk6') { response.writeHead(401).end(); return; }
    if (request.url !== `/api/opportunities/${encodeURIComponent(opportunityId)}`) { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({
      data: { id: opportunityId, name: 'Sample Candidate', stage: '00922a60-7c15-422b-b086-f62000824fd7' },
    }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Lever fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-lever-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { apiKey: new SecretString('sample-lever-api-key') };
  const provider = new LeverClient({ baseUrl: `http://127.0.0.1:${address.port}/api` }, {
    resolve: async () => credentials,
  });
  const connection = newConnection(provider, { provider: 'lever', name: ConnectionName });
  const flow = new OpportunityInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `lever-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { opportunityId });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<OpportunityStage>().id).toBe(opportunityId);
    expect(result.singleOutput<OpportunityStage>().stage).toBe('00922a60-7c15-422b-b086-f62000824fd7');
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
