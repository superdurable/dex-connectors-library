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
import { PersonioClient } from '../../src/client.js';
import { newConnection, type PersonProfile, type Credentials } from '../../src/index.js';
import { ConnectionName, PersonInspectionFlow } from '../../examples/person-inspection/flow.js';

it('retries a provider 503 and persists the person through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const personId = '3003';
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer sample-access-token') { response.writeHead(401).end(); return; }
    if (request.url !== `/v2/persons/${personId}`) { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ id: personId, first_name: 'Jane', last_name: 'Doe' }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Personio fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-personio-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { clientId: 'sample-client-id', clientSecret: new SecretString('sample-client-secret'), accessToken: new SecretString('sample-access-token') };
  const provider = new PersonioClient({ baseUrl: `http://127.0.0.1:${address.port}/v2` }, new StaticCredentialProvider([[{ provider: 'personio', name: ConnectionName }, credentials]]));
  const connection = newConnection(provider, { provider: 'personio', name: ConnectionName });
  const flow = new PersonInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `personio-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { personId });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<PersonProfile>().id).toBe(personId);
    expect(result.singleOutput<PersonProfile>()).toEqual({ id: personId, firstName: 'Jane', lastName: 'Doe' });
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
