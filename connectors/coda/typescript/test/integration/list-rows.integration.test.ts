// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer, type Server } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { CodaClient, newConnection, type ListRowsOutput } from '../../src/index.js';
import { ConnectionName, ListRowsFlow } from '../../examples/list-rows/flow/workflow.js';

it('runs a Coda query through real Dex and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer test-token') { response.writeHead(401).end(); return; }
    if (request.method !== 'GET' || !request.url?.startsWith('/apis/v1/docs/AbCDeFGH/tables/grid-pqRst-U/rows')) {
      response.writeHead(404).end(); return;
    }
    if (request.url.includes('pageToken=REJECT')) {
      response.writeHead(403, { 'Content-Type': 'application/json' }).end('{"message":"private sentinel"}');
      return;
    }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end('{"items":[{"id":"i-Meridian","name":"Meridian","values":{"c-Name":"Meridian"}}],"nextPageToken":""}');
  });
  const port = await bindFixturePort(server);
  const directory = await mkdtemp(join(tmpdir(), 'dex-coda-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new CodaClient({ baseUrl: `http://127.0.0.1:${port}/apis/v1` }, {
    resolve: async () => ({ apiToken: new SecretString('test-token') }),
  });
  const flow = new ListRowsFlow(newConnection(provider, { provider: 'coda', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const input = { docId: 'AbCDeFGH', tableId: 'grid-pqRst-U', limit: 2 };
    const successId = `coda-list-${randomUUID()}`;
    await client.startFlow(flow, successId, input);
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect((success.singleOutput<ListRowsOutput>().data as { items: { id: string }[] }).items[0]?.id).toBe('i-Meridian');
    const rejectionId = `coda-list-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { ...input, pageToken: 'REJECT' });
    const rejected = await client.waitForFlow(rejectionId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close();
    await worker.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);

async function bindFixturePort(server: Server): Promise<number> {
  for (let port = 21070; port <= 21099; port++) {
    const isListening = await new Promise<boolean>(resolve => {
      const onError = (): void => { server.off('listening', onListening); resolve(false); };
      const onListening = (): void => { server.off('error', onError); resolve(true); };
      server.once('error', onError);
      server.once('listening', onListening);
      server.listen(port, '127.0.0.1');
    });
    if (isListening) return port;
  }
  throw new Error('no Coda fixture port available in 21070-21099');
}
