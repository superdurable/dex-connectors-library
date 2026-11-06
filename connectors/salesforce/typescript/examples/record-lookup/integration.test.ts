// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { spawn, type ChildProcess } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { afterAll, beforeAll, expect, it } from 'vitest';
import { Client, Registry, Worker, openBlobCache, type BlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SalesforceClient, newConnection } from '@superdurable/connector-salesforce';
import { ConnectionName, SalesforceRecordLookupFlow } from './flow.js';

const dexPort = 40830;
const webPort = 40831;
const recordId = '003RM000001AbcdYAC';
const serviceAddress = `127.0.0.1:${dexPort}`;
const requests: ProviderRequest[] = [];
let providerStatus = 200;
let processHandle: ChildProcess;
let dataDirectory: string;
let cacheDirectory: string;
let cache: BlobCache;
let worker: Worker;
let client: Client;
let flow: SalesforceRecordLookupFlow;

async function awaitDexService(): Promise<void> {
  const deadline = Date.now() + 45_000;
  while (Date.now() < deadline) {
    if (processHandle.exitCode !== null) throw new Error('dexcli dev exited before readiness');
    try {
      const socket = await import('node:net');
      await new Promise<void>((resolve, reject) => {
        const connection = socket.createConnection({ host: '127.0.0.1', port: dexPort });
        connection.once('connect', () => { connection.destroy(); resolve(); });
        connection.once('error', reject);
      });
      return;
    } catch { await delay(100); }
  }
  throw new Error('dexcli dev did not open its gRPC port');
}

beforeAll(async () => {
  dataDirectory = await mkdtemp(join(tmpdir(), 'salesforce-ts-dex-'));
  cacheDirectory = await mkdtemp(join(tmpdir(), 'salesforce-ts-cache-'));
  processHandle = spawn('dexcli', [
    'dev', '-dex-port', String(dexPort), '-web-port', String(webPort),
    '-open=false', '-zd-data-dir', dataDirectory,
  ], { stdio: 'ignore' });
  await awaitDexService();
  const reference = { provider: 'salesforce', name: ConnectionName };
  const credentials = {
    authMethodId: 'salesforce-oauth', oauthClientId: 'inert-client-id',
    accessToken: new SecretString('inert-salesforce-access'), instanceUrl: 'http://127.0.0.1:12345',
  };
  const transport = async (request: ProviderRequest): Promise<ProviderResponse> => {
    requests.push(request);
    const body = providerStatus === 200
      ? { attributes: { type: 'Contact' }, Id: recordId, Email: 'priya@example.com' }
      : [{ errorCode: 'NOT_FOUND' }];
    return { status: providerStatus, headers: {}, body: Buffer.from(JSON.stringify(body)) };
  };
  const provider = new StaticCredentialProvider([[reference, credentials]]);
  flow = new SalesforceRecordLookupFlow(newConnection(new SalesforceClient({}, provider, { send: transport }), reference));
  const registry = new Registry([flow]);
  cache = openBlobCache({ directory: cacheDirectory, maxBytes: 64 * 1024 * 1024 });
  worker = new Worker(registry, cache, { serverAddress: serviceAddress });
  client = new Client(registry, cache, { serverAddress: serviceAddress });
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000 });
}, 90_000);

afterAll(async () => {
  if (client) await client.close();
  if (worker) await worker.close();
  if (cache) cache.close();
  if (processHandle && processHandle.exitCode === null) {
    processHandle.kill('SIGTERM');
    await new Promise<void>(resolve => processHandle.once('exit', () => resolve()));
  }
  if (cacheDirectory) await rm(cacheDirectory, { recursive: true, force: true });
  if (dataDirectory) await rm(dataDirectory, { recursive: true, force: true });
});

it('completes the checked-in Flow through a real Dex Worker', async () => {
  providerStatus = 200;
  requests.length = 0;
  const flowId = `salesforce-ts-read-${randomUUID()}`;
  await client.startFlow(flow, flowId, { sObjectType: 'Contact', recordId, fields: ['Id', 'Email'] });
  const result = await client.waitForFlow(flowId, 40_000);
  expect(result.status).toBe('completed');
  expect(result.singleOutput()).toMatchObject({ branch: 'found', value: { id: recordId, fields: { Email: 'priya@example.com' } } });
  expect(requests).toHaveLength(1);
  expect(new URL(requests[0]!.url).pathname).toBe(`/services/data/v62.0/sobjects/Contact/${recordId}`);
}, 50_000);

it('fails on an unwired optional notFound branch', async () => {
  providerStatus = 404;
  requests.length = 0;
  const flowId = `salesforce-ts-missing-${randomUUID()}`;
  await client.startFlow(flow, flowId, { sObjectType: 'Contact', recordId, fields: ['Id', 'Email'] });
  const result = await client.waitForFlow(flowId, 40_000);
  expect(result.status).toBe('failed');
  expect(requests).toHaveLength(1);
}, 50_000);
