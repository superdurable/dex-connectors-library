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
import { RabbitMQClient } from '../../src/client.js';
import { newConnection, type QueueMetrics, type Credentials } from '../../src/index.js';
import { ConnectionName, QueueInspectionFlow } from '../../examples/queue-inspection/flow.js';

it('retries a provider 503 and persists the worker through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Basic b2JzZXJ2ZXI6c2FtcGxlLXBhc3N3b3Jk') { response.writeHead(401).end(); return; }
    if (request.url !== '/api/queues/%2F/work') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({
      vhost: '/', name: 'work', messages: 3, consumers: 1, state: 'running',
    }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('RabbitMQ fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-rabbitmq-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { username: 'observer', password: new SecretString('sample-password') };
  const provider = new RabbitMQClient({ managementUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => credentials,
  });
  const connection = newConnection(provider, { provider: 'rabbitmq', name: ConnectionName });
  const flow = new QueueInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `rabbitmq-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { virtualHost: '/', name: 'work' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<QueueMetrics>()).toEqual({ virtualHost: '/', name: 'work', messages: 3, consumers: 1, state: 'running' });
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
