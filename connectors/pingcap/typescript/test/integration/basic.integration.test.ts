// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { PingcapClient } from '../../src/client.js';
import { newConnection, ListProjectsInputSchema, type Credentials } from '../../src/index.js';
import { ConnectionName, PingcapInspectionFlow } from '../../examples/basic/flow.js';

it('retries provider 503 and persists the result through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const scenarios = JSON.parse(await readFile(new URL('../../../../../conformance/connectors/pingcap/scenarios.json', import.meta.url), 'utf8')) as { scenarios: Array<{ name: string; input: object; exchanges: Array<{ status?: number; headers?: Record<string, string>; body?: string }>; expected: { output: unknown } }> };
  const fixture = scenarios.scenarios.find(scenario => scenario.name === 'listProjects-listed');
  if (!fixture) throw new Error('missing Go-recorded happy-path fixture');
  let attempts = 0;
  const server = createServer((_request, response) => {
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    if (attempts === 2) { const exchange = fixture.exchanges[0]!; response.writeHead(exchange.status ?? 401, exchange.headers ?? {}).end(exchange.body ?? ''); return; }
    const exchange = fixture.exchanges.at(-1)!;
    response.writeHead(exchange.status ?? 200, exchange.headers ?? {}).end(exchange.body ?? '');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Pingcap fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-pingcap-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { publicKey: new SecretString('test-public-key'), privateKey: new SecretString('test-private-key') };
  const provider = new PingcapClient({}, { resolve: async () => credentials }, { send: async request => {
    const target = new URL(request.url);
    const reply = await fetch(`http://127.0.0.1:${address.port}${target.pathname}${target.search}`, { method: request.method, headers: request.headers, body: request.body ? Buffer.from(request.body) : undefined });
    return { status: reply.status, headers: Object.fromEntries(reply.headers), body: new Uint8Array(await reply.arrayBuffer()) };
  } });
  const connection = newConnection(provider, { provider: 'pingcap', name: ConnectionName });
  const flow = new PingcapInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `pingcap-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, ListProjectsInputSchema.parse(fixture.input));
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual(fixture.expected.output);
    expect(attempts).toBe(3);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
