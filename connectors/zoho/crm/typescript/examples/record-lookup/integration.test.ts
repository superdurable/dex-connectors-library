// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { randomUUID } from 'node:crypto';
import { afterAll, beforeAll, expect, it } from 'vitest';
import { Client, Registry, Worker, openBlobCache, type BlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { Connection, ZohoClient, type Credentials } from '@superdurable/connector-zoho-crm';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ConnectionName, ZohoRecordLookupFlow } from './flow.js';

let cacheDirectory: string, cache: BlobCache, worker: Worker, client: Client;
let requestCount = 0;
const reference = { provider: 'zoho', name: ConnectionName };
const credentials: Credentials = { authMethodId: 'zoho-us-oauth', apiDomain: 'https://www.zohoapis.com', accessToken: new SecretString('test-access'),
  oauthClientId: 'test-client', oauthClientSecret: new SecretString('test-secret'), refreshToken: new SecretString('test-refresh') };
const recordId = '4150868000000376008';
const transport = async (request: ProviderRequest): Promise<ProviderResponse> => {
  requestCount++;
  if (request.url.endsWith('/4150868000000000000')) return { status: 204, headers: {}, body: Buffer.alloc(0) };
  if (requestCount === 1) return { status: 503, headers: {}, body: Buffer.from('{"code":"TEMPORARY"}') };
  return { status: 200, headers: {}, body: Buffer.from(JSON.stringify({ data: [{ id: recordId, Email: 'ada@example.com' }] })) };
};
const provider = new StaticCredentialProvider([[reference, credentials]]);
const refreshDriver = { refreshRequired: () => false, refresh: async () => { throw new TypeError('unexpected credential refresh'); } };
const flow = new ZohoRecordLookupFlow(new Connection(reference, new ZohoClient({}, provider,
  { apiBaseURL: 'http://127.0.0.1:12345/crm/v8', transport, refreshDriver })));
const registry = new Registry([flow]);

beforeAll(async () => {
  cacheDirectory = await mkdtemp(join(tmpdir(), 'zoho-crm-dex-integration-'));
  cache = openBlobCache({ directory: cacheDirectory, maxBytes: 64 * 1024 * 1024 });
  worker = new Worker(registry, cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS ?? '127.0.0.1:8801' });
  client = new Client(registry, cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS ?? '127.0.0.1:8801' });
  await worker.start(); await worker.waitPullRegistered({ timeoutMs: 15_000 });
}, 30_000);

afterAll(async () => { if (client) await client.close(); if (worker) await worker.close(); if (cache) cache.close(); if (cacheDirectory) await rm(cacheDirectory, { recursive: true, force: true }); });

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
