// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { createServer } from 'node:http';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client as DexClient, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { Client as SmartsheetClient } from '../../src/client.js';
import { newConnection } from '../../src/connection.js';
import { ConnectionName, SmartsheetLookupFlow, type Outcome } from './flow.js';

/** Verify both provider reads through a real Dex Worker and Client. */
export async function verifySmartsheetLookup(serverAddress: string): Promise<void> {
  const requests: string[] = [];
  let listAttempts = 0;
  const provider = createServer((request, response) => {
    requests.push(request.url ?? '');
    assert.equal(request.headers.authorization, 'Bearer test-secret-key');
    response.setHeader('Content-Type', 'application/json');
    if (request.url === '/2.0/sheets?page=1&pageSize=10') {
      listAttempts++;
      if (listAttempts === 1) { response.writeHead(429); response.end('{"error":"rate limit"}'); return; }
      response.end('{"pageNumber":1,"totalCount":1,"data":[{"id":1001,"name":"Meridian Planning"}]}');
      return;
    }
    if (request.url === '/2.0/sheets/1001') { response.end('{"id":1001,"name":"Meridian Planning","columns":[{"id":2001,"title":"Task"}],"rows":[{"id":3001,"cells":[{"columnId":2001,"value":1.10},{"columnId":2001,"value":9007199254740993}]}]}'); return; }
    response.writeHead(404); response.end('{}');
  });
  await new Promise<void>(resolve => provider.listen(0, '127.0.0.1', resolve));
  const address = provider.address();
  if (!address || typeof address === 'string') throw new Error('provider test server has no address');
  const directory = await mkdtemp(join(tmpdir(), 'dex-smartsheet-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 1 << 30 });
  const connection = newConnection(new SmartsheetClient({ baseUrl: `http://127.0.0.1:${address.port}/2.0` }, new StaticCredentialProvider([[{ provider: 'smartsheet', name: ConnectionName }, { apiKey: new SecretString('test-secret-key') }]])), ConnectionName);
  const flow = new SmartsheetLookupFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new DexClient(registry, cache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });
    const flowId = `smartsheet-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { page: 1 });
    const completed = await client.waitForFlow(flowId, 30_000);
    assert.equal(completed.status, 'completed', completed.errorMessage);
    const sheet = completed.singleOutput<Outcome>().sheet;
    assert.equal(sheet?.id, 1001);
    assert.equal(sheet?.rows?.[0]?.cells?.length, 2);
    assert.deepEqual(requests, ['/2.0/sheets?page=1&pageSize=10', '/2.0/sheets?page=1&pageSize=10', '/2.0/sheets/1001']);
  } finally {
    await client.close();
    await worker.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>((resolve, reject) => provider.close(error => error ? reject(error) : resolve()));
  }
}

if (process.argv[1]?.endsWith('/integration.ts')) {
  verifySmartsheetLookup(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => { console.error(error); process.exitCode = 1; });
}
