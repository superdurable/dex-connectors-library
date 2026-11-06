// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { randomUUID } from 'node:crypto';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeAll, expect, it } from 'vitest';
import { Client, Registry, Worker, openBlobCache, type BlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { Connection, ZohoClient, type Credentials } from '@superdurable/connector-zoho-crm';
import { ConnectionName, ZohoRecordLookupFlow } from './flow.js';

let cacheDirectory: string;
let cache: BlobCache;
let worker: Worker;
let client: Client;
let flow: ZohoRecordLookupFlow;
let providerServer: Server;
let requestCount = 0;
const reference = { provider: 'zoho', name: ConnectionName };
const credentials: Credentials = {
  authMethodId: 'zoho-us-oauth', apiDomain: 'https://www.zohoapis.com',
  accessToken: new SecretString('test-access'), oauthClientId: 'test-client',
  oauthClientSecret: new SecretString('test-secret'), refreshToken: new SecretString('test-refresh'),
};
const recordId = '4150868000000376008';

beforeAll(async () => {
  const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!serverAddress) throw new TypeError('DEX_FLOW_SERVICE_ADDRESS must name the running dexcli dev server');
  providerServer = createServer((request, response) => {
    requestCount++;
    response.setHeader('Content-Type', 'application/json');
    if (request.url?.endsWith('/4150868000000000000')) { response.writeHead(204).end(); return; }
    if (requestCount === 1) { response.writeHead(503).end('{"code":"TEMPORARY"}'); return; }
    response.writeHead(200).end(JSON.stringify({ data: [{ id: recordId, Email: 'ada@example.com' }] }));
  });
  await new Promise<void>((resolve, reject) => {
    providerServer.once('error', reject);
    providerServer.listen(0, '127.0.0.1', resolve);
  });
  const port = (providerServer.address() as AddressInfo).port;
  const provider = new StaticCredentialProvider([[reference, credentials]]);
  const refreshDriver = { refreshRequired: () => false, refresh: async () => { throw new TypeError('unexpected credential refresh'); } };
  flow = new ZohoRecordLookupFlow(new Connection(reference, new ZohoClient({}, provider,
    { apiBaseURL: `http://127.0.0.1:${port}/crm/v8`, refreshDriver })));
  const registry = new Registry([flow]);
  cacheDirectory = await mkdtemp(join(tmpdir(), 'zoho-crm-dex-integration-'));
  cache = openBlobCache({ directory: cacheDirectory, maxBytes: 64 * 1024 * 1024 });
  worker = new Worker(registry, cache, { serverAddress });
  client = new Client(registry, cache, { serverAddress });
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 15_000 });
}, 30_000);

afterAll(async () => {
  if (client) await client.close();
  if (worker) await worker.close();
  if (cache) cache.close();
  if (providerServer) await new Promise<void>((resolve, reject) => providerServer.close(error => error ? reject(error) : resolve()));
  if (cacheDirectory) await rm(cacheDirectory, { recursive: true, force: true });
});

it('retries a 503 read and completes with the record through Dex', async () => {
  const flowId = `zoho-crm-read-${randomUUID()}`;
  await client.startFlow(flow, flowId, { module: 'Contacts', recordId, fields: ['Email'] });
  const result = await client.waitForFlow(flowId, 20_000);
  expect(result.status).toBe('completed');
  expect(result.singleOutput()).toMatchObject({ branch: 'found', value: { id: recordId, fields: { Email: 'ada@example.com' } } });
  expect(requestCount).toBe(2);
}, 30_000);

it('fails on an unwired optional notFound branch', async () => {
  const flowId = `zoho-crm-missing-${randomUUID()}`;
  await client.startFlow(flow, flowId, { module: 'Contacts', recordId: '4150868000000000000' });
  const result = await client.waitForFlow(flowId, 20_000);
  expect(result.status).toBe('failed');
}, 30_000);
