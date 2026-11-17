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
import { DadaClient, signRequest, newConnection, type OrderStatus } from '../../src/index.js';
import { DadaOrderStatusFlow, ConnectionName, orderStatusAttribute } from '../../examples/order-status/flow.js';

it('retries a signed 503 through a real Dex Worker and persists only order status', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    if (request.method !== 'POST' || request.url !== '/api/order/status/query' || request.headers['content-type'] !== 'application/json; charset=utf-8') { response.writeHead(400).end(); return; }
    const chunks: Buffer[] = [];
    request.on('data', chunk => chunks.push(Buffer.from(chunk)));
    request.on('end', () => {
      let envelope: Record<string, unknown>;
      try { envelope = JSON.parse(Buffer.concat(chunks).toString()) as Record<string, unknown>; }
      catch { response.writeHead(400).end(); return; }
      const body = '{"order_id":"20170301000001"}';
      if (envelope.app_key !== '123456' || envelope.body !== body || envelope.format !== 'json' || envelope.source_id !== '73753' || envelope.timestamp !== '1488363493' || envelope.v !== '1.0' || envelope.signature !== signRequest('abcdefg123', '123456', body, '73753', '1488363493')) { response.writeHead(401).end(); return; }
      requests++;
      if (requests === 1) { response.writeHead(503, { 'Retry-After': '1' }).end(); return; }
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ status: 'success', code: 0, result: { orderId: '20170301000001', statusCode: 1, statusMsg: '待接单', riderName: 'private rider', riderPhone: 'private phone' } }));
    });
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-dada-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new DadaClient({ baseUrl: `http://127.0.0.1:${address.port}` }, { resolve: async () => ({ appKey: '123456', appSecret: new SecretString('abcdefg123'), sourceID: '73753' }) }, { now: () => new Date(1488363493 * 1000) });
  const flow = new DadaOrderStatusFlow(newConnection(provider, { provider: 'dada', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `dada-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { orderID: '20170301000001' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const expected: OrderStatus = { orderID: '20170301000001', statusCode: 1, statusMessage: '待接单' };
    expect(result.singleOutput<OrderStatus>()).toEqual(expected);
    const [persistedStatus] = await client.getAttributes(flowId, orderStatusAttribute);
    expect(persistedStatus).toEqual(expected);
    expect(JSON.stringify(result.singleOutput<OrderStatus>())).not.toContain('rider');
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
