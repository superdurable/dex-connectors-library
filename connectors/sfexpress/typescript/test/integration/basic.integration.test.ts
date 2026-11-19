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
import { SFExpressClient } from '../../src/client.js';
import { newConnection } from '../../src/index.js';
import { ConnectionName, SFExpressInspectionFlow } from '../../examples/basic/flow.js';

it('retries one 503 and persists the provider result through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const bodies = [{ apiResultCode: "A1000", apiResultData: JSON.stringify({ success: true, errorCode: "S0000", msgData: { routeResps: [{ mailNo: "SF1234567890123", routes: [{ acceptTime: "2026-11-13 09:30:00", remark: "Collected", opcode: "50" }] }] } }) }];
  const server = createServer((request, response) => {
    requests++;
    if (requests === 1) { response.writeHead(503).end(); return; }
    const selected = bodies[0];
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify(selected));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('SFExpress fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-sfexpress-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new SFExpressClient({}, { resolve: async () => ({ partnerId: 'customer-1', checkword: new SecretString('private checkword') }) }, {

    send: async request => {
      const reply = await fetch(`http://127.0.0.1:${address.port}${new URL(request.url).pathname}`, { method: request.method, headers: request.headers, body: request.body ? Buffer.from(request.body) : undefined });
      return { status: reply.status, headers: Object.fromEntries(reply.headers), body: new Uint8Array(await reply.arrayBuffer()) };
    },
  });
  const connection = newConnection(provider, { provider: 'sfexpress', name: ConnectionName });
  const flow = new SFExpressInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `sfexpress-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { waybillNumber: 'SF1234567890123' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({ waybillNumber: "SF1234567890123", routes: [{ occurredAt: "2026-11-13 09:30:00", opcode: "50" }] });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
