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
import { ChuanglanClient, newConnection, type GetBalanceOutput } from '../../src/index.js';
import { ChuanglanBalanceFlow, ConnectionName, balanceAttribute } from '../../examples/balance/flow.js';

it('retries a 503 through a real Dex Worker and persists the balance', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer(async (request, response) => {
    if (request.url !== '/msg/balance/json' || request.method !== 'POST') { response.writeHead(404).end(); return; }
    const chunks: Buffer[] = []; for await (const chunk of request) chunks.push(Buffer.from(chunk));
    const body = JSON.parse(Buffer.concat(chunks).toString()) as { account?: string; password?: string };
    if (body.account !== 'N9000123' || body.password !== 'secret-password') { response.writeHead(401).end(); return; }
    requests++;
    if (requests === 1) { response.writeHead(503, { 'Retry-After': '1' }).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ code: 0, balance: '953', time: '20180524170449', errorMsg: '' }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-chuanglan-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new ChuanglanClient({ baseUrl: `http://127.0.0.1:${address.port}` }, { resolve: async () => ({ account: new SecretString('N9000123'), password: new SecretString('secret-password') }) });
  const flow = new ChuanglanBalanceFlow(newConnection(provider, { provider: 'chuanglan', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `chuanglan-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<GetBalanceOutput>()).toEqual({ remainingMessages: 953 });
    const [persistedBalance] = await client.getAttributes(flowId, balanceAttribute);
    expect(persistedBalance).toEqual({ remainingMessages: 953 });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
