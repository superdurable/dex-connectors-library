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
import { DieyuyunClient, newConnection, type GetAccountBalanceOutput } from '../../src/index.js';
import { DieyuyunBalanceFlow, ConnectionName, balanceAttribute } from '../../examples/balance/flow.js';

it('retries a 503 through a real Dex Worker and persists exact CNY decimal text', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    if (request.method !== 'GET' || request.url !== '/v1/balance' || request.headers.authorization !== 'Bearer private-test-key' || request.headers.accept !== 'application/json') { response.writeHead(401).end(); return; }
    requests++;
    if (requests === 1) { response.writeHead(503, { 'Retry-After': '1' }).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end('{"object":"balance","currency":"CNY","balance":12345678901234567890.12345678901234567890,"api_key":{"name":"private key name","daily_budget":20}}');
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-dieyuyun-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new DieyuyunClient({ baseUrl: `http://127.0.0.1:${address.port}` }, { resolve: async () => ({ apiKey: new SecretString('private-test-key') }) });
  const flow = new DieyuyunBalanceFlow(newConnection(provider, { provider: 'dieyuyun', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `dieyuyun-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const expected: GetAccountBalanceOutput = { availableCNY: '12345678901234567890.12345678901234567890' };
    expect(result.singleOutput<GetAccountBalanceOutput>()).toEqual(expected);
    const [persistedBalance] = await client.getAttributes(flowId, balanceAttribute);
    expect(persistedBalance).toEqual(expected);
    expect(JSON.stringify(result.singleOutput<GetAccountBalanceOutput>())).not.toContain('private key name');
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
