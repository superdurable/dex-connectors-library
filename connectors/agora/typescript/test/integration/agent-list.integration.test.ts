// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { once } from 'node:events';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { AgoraClient } from '../../src/client.js';
import { newConnection, type RunningAgents } from '../../src/index.js';
import { ConnectionName, RunningAgentsFlow } from '../../examples/agent-list/flow.js';

it('retries Agora 503 through a real Dex Worker and persists the agent page', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    const validPath = request.url === '/api/conversational-ai-agent/v2/projects/1234567890abcdef1234567890abcdef/agents';
    const validAuthorization = request.headers.authorization === `Basic ${Buffer.from('customer-id:customer-secret').toString('base64')}`;
    if (request.method !== 'GET' || !validPath || !validAuthorization) { response.writeHead(400).end(); return; }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ status: 'ok', data: { count: 1, list: [{ agent_id: 'agent-123', status: 'RUNNING', start_ts: 1735035893 }] }, meta: { total: 1, cursor: '' } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Agora fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-agora-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new AgoraClient({ baseUrl: `http://127.0.0.1:${address.port}` }, { resolve: async () => ({ appId: '1234567890abcdef1234567890abcdef', customerId: 'customer-id', customerSecret: new SecretString('customer-secret') }) });
  const flow = new RunningAgentsFlow(newConnection(provider, { provider: 'agora', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `agora-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<RunningAgents>()).toEqual({ count: 1, agents: [{ agentId: 'agent-123', status: 'RUNNING', startTimestamp: 1735035893 }], total: 1, cursor: '' });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
