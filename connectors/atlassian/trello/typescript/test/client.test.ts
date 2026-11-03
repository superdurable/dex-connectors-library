// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { TrelloClient } from '../src/client.js';

const apiKey = '0123456789abcdef0123456789abcdef';
const token = 'ATTAtrelloTestToken0123456789abcdef';
const boardId = '6512f0a1c2d3e4f5a6b70001';
const call = {
  id: 'e12fc5cf-e5e0-5092-b80f-f16501ac5593',
  connection: { provider: 'trello', name: 'trello-test' },
  operation: { connectorId: 'trello', operationId: 'listCards' },
  context: { cancellationSignal: new AbortController().signal },
} as Call;

it('keeps credentials in the Authorization header and rejects reflected secrets', async () => {
  let requests = 0;
  const client = new TrelloClient({}, { resolve: async () => ({ apiKey: new SecretString(apiKey), token: new SecretString(token) }) }, {
    send: async request => {
      requests++;
      expect(request.url).not.toContain(apiKey);
      expect(request.url).not.toContain(token);
      expect(request.headers?.Authorization).toContain(apiKey);
      expect(request.headers?.Authorization).toContain(token);
      return { status: 200, headers: {}, body: Buffer.from(`{"cards":"${token}"}`) };
    },
  });
  const attempt = await client.listCards().invoke(call, { boardId });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind === 'branch') expect(attempt.branch).toBe('invalidResponse');
  expect(JSON.stringify(attempt)).not.toContain(apiKey);
  expect(JSON.stringify(attempt)).not.toContain(token);
  expect(requests).toBe(1);
});

it('rejects unsafe credential header characters before contacting Trello', async () => {
  let requests = 0;
  const client = new TrelloClient({}, { resolve: async () => ({ apiKey: new SecretString(`${apiKey}"`), token: new SecretString(token) }) }, {
    send: async () => { requests++; throw new Error('unexpected request'); },
  });
  const attempt = await client.listCards().invoke(call, { boardId });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind === 'branch') {
    expect(attempt.branch).toBe('defect');
    expect(attempt.failure?.kind).toBe('AUTHENTICATION');
  }
  expect(requests).toBe(0);
});
