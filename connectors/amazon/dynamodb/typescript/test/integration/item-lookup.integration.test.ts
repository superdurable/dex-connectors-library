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
import { AmazonDynamoDBClient } from '../../src/client.js';
import { newConnection, type ItemResult, type Credentials } from '../../src/index.js';
import { ConnectionName, ItemLookupFlow } from '../../examples/item-lookup/flow.js';

it('retries a provider 503 and persists the item through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (!request.headers.authorization?.includes('/eu-west-1/dynamodb/aws4_request')) { response.writeHead(401).end(); return; }
    if (request.method !== 'POST' || request.url !== '/' || request.headers['x-amz-target'] !== 'DynamoDB_20120810.GetItem') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ Item: { pk: { S: 'example' } } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Amazon DynamoDB fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-dynamodb-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { accessKeyId: new SecretString('AKIADYNAMOCONNECTOR'), secretAccessKey: new SecretString('dynamodb-test-secret'), sessionToken: new SecretString('') };
  const provider = new AmazonDynamoDBClient({ endpoint: `http://127.0.0.1:${address.port}`, region: 'eu-west-1' }, {
    resolve: async () => credentials,
  });
  const connection = newConnection(provider, { provider: 'amazon-dynamodb', name: ConnectionName });
  const flow = new ItemLookupFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `dynamodb-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { tableName: 'Examples', key: { pk: { S: 'example' } }, consistentRead: false });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<ItemResult>().item).toEqual({ pk: { S: 'example' } });
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
