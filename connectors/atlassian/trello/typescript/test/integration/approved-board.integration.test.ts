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
import { TrelloClient } from '../../src/client.js';
import { newConnection, type ListCardsOutput } from '../../src/index.js';
import { ConnectionName, ListCardsFlow } from '../../examples/approved-board-cards/flow/workflow.js';

const approvedBoardId = '6512f0a1c2d3e4f5a6b70001';
const rejectedBoardId = '6512f0a1c2d3e4f5a6b70002';
const cardId = '6512f0a1c2d3e4f5a6b72001';
const listId = '6512f0a1c2d3e4f5a6b70101';

it('lists Trello cards through real Dex and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'OAuth oauth_consumer_key="0123456789abcdef0123456789abcdef", oauth_token="ATTAtrelloTestToken0123456789abcdef"') {
      response.writeHead(401).end(); return;
    }
    if (request.url?.startsWith(`/1/boards/${rejectedBoardId}/cards`)) { response.writeHead(403).end('permission denied'); return; }
    if (request.method !== 'GET' || !request.url?.startsWith(`/1/boards/${approvedBoardId}/cards`)) { response.writeHead(404).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify([{ id: cardId, name: 'Approved request', idBoard: approvedBoardId, idList: listId, closed: false, dueComplete: false, pos: 1024 }]));
  });
  for (let port = 21050; port <= 21099; port++) {
    try { server.listen(port, '127.0.0.1'); await once(server, 'listening'); break; }
    catch (error) { if (port === 21099 || (error as NodeJS.ErrnoException).code !== 'EADDRINUSE') throw error; }
  }
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Trello fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-trello-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new TrelloClient({ endpoint: `http://127.0.0.1:${address.port}/1` }, {
    resolve: async () => ({ apiKey: new SecretString('0123456789abcdef0123456789abcdef'), token: new SecretString('ATTAtrelloTestToken0123456789abcdef') }),
  });
  const flow = new ListCardsFlow(newConnection(provider, { provider: 'trello', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `trello-list-${randomUUID()}`;
    await client.startFlow(flow, successId, { boardId: approvedBoardId, pageSize: 10 });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<ListCardsOutput>().cards?.[0]?.id).toBe(cardId);
    const rejectedId = `trello-list-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { boardId: rejectedBoardId, pageSize: 10 });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
