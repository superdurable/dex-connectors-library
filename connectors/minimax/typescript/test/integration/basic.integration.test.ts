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
import { MiniMaxClient } from '../../src/client.js';
import { newConnection, type Credentials } from '../../src/index.js';
import { ConnectionName, MiniMaxInspectionFlow } from '../../examples/basic/flow.js';

it('retries a provider 503 and persists the result through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((_request, response) => {
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ base_resp: { status_code: 0 }, model_remains: [{ model_name: 'MiniMax-M*', current_interval_remaining_percent: 85, current_weekly_remaining_percent: 97, end_time: 1776373200000, weekly_end_time: 1776614400000, current_interval_status: 1 }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('MiniMax fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-minimax-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { apiKey: new SecretString('sk-cp-test-minimax-key') };
  const provider = new MiniMaxClient({}, { resolve: async () => credentials }, { send: async request => {
    const target = new URL(request.url);
    const reply = await fetch(`http://127.0.0.1:${address.port}${target.pathname}${target.search}`, { method: request.method, headers: request.headers, body: request.body ? Buffer.from(request.body) : undefined });
    return { status: reply.status, headers: Object.fromEntries(reply.headers), body: new Uint8Array(await reply.arrayBuffer()) };
  } });
  const connection = newConnection(provider, { provider: 'minimax', name: ConnectionName });
  const flow = new MiniMaxInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `minimax-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({"modelGroups":[{"modelName":"MiniMax-M*","intervalRemainingPercent":85,"weeklyRemainingPercent":97,"intervalResetAtMillis":1776373200000,"weeklyResetAtMillis":1776614400000,"intervalStatus":1}]});
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
