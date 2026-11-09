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
import { GreenhouseClient } from '../../src/client.js';
import { newConnection, type CandidateProfile, type Credentials } from '../../src/index.js';
import { ConnectionName, CandidateInspectionFlow } from '../../examples/candidate-inspection/flow.js';

it('retries a provider 503 and persists the candidate through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const candidateId = 53883394;
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer sample-access-token') { response.writeHead(401).end(); return; }
    if (!request.url?.startsWith('/v3/candidates?')) { response.writeHead(404).end(); return; }
    const query = new URL(request.url, 'http://localhost').searchParams;
    expect(Object.fromEntries(query)).toEqual({ fields: 'id,first_name,last_name', ids: String(candidateId), per_page: '1' });
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify([{ id: candidateId, first_name: 'Sample', last_name: 'Candidate' }]));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Greenhouse fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-greenhouse-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { clientId: 'sample-client-id', clientSecret: new SecretString('sample-client-secret'), accessToken: new SecretString('sample-access-token') };
  const provider = new GreenhouseClient({ baseUrl: `http://127.0.0.1:${address.port}/v3` }, new StaticCredentialProvider([[{ provider: 'greenhouse', name: ConnectionName }, credentials]]));
  const connection = newConnection(provider, { provider: 'greenhouse', name: ConnectionName });
  const flow = new CandidateInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `greenhouse-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { candidateId });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<CandidateProfile>().id).toBe(candidateId);
    expect(result.singleOutput<CandidateProfile>()).toEqual({ id: candidateId, firstName: 'Sample', lastName: 'Candidate' });
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
