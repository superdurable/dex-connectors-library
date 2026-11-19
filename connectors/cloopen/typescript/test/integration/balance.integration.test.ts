// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { CloopenClient, newConnection, type GetBalanceOutput } from '../../src/index.js';
import { CloopenBalanceFlow, ConnectionName, balanceAttribute } from '../../examples/balance/flow.js';

const accountSid = 'abcdefghijklmnopqrstuvwxyz012345';
const authToken = '012345abcdefghijklmnopqrstuvwxyz';
const fixedTime = new Date('2026-11-16T12:34:56Z');
const timestamp = '20261116123456';

it('retries a signed 503 through a real Dex Worker and persists the balance', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    const signature = createHash('md5').update(accountSid + authToken + timestamp).digest('hex').toUpperCase();
    if (request.method !== 'GET' || request.url !== `/2013-12-26/Accounts/${accountSid}/AccountInfo?sig=${signature}` ||
      request.headers.authorization !== Buffer.from(`${accountSid}:${timestamp}`).toString('base64')) { response.writeHead(401).end(); return; }
    requests++;
    if (requests === 1) { response.writeHead(503, { 'Retry-After': '1' }).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ statusCode: '000000', Account: { balance: '211.00', subBalance: '112' } }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-cloopen-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new CloopenClient({ baseUrl: `http://127.0.0.1:${address.port}` }, { resolve: async () => ({ accountSid: new SecretString(accountSid), authToken: new SecretString(authToken) }) }, { now: () => fixedTime });
  const flow = new CloopenBalanceFlow(newConnection(provider, { provider: 'cloopen', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `cloopen-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<GetBalanceOutput>()).toEqual({ balance: '211.00' });
    const [persistedBalance] = await client.getAttributes(flowId, balanceAttribute);
    expect(persistedBalance).toEqual({ balance: '211.00' });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
