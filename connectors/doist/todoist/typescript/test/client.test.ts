// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { TodoistClient } from '../src/client.js';

const call = {
  id: 'body-limit', context: { flowId: 'body-limit', cancellationSignal: new AbortController().signal },
} as Call;

it('classifies oversized rejected bodies by HTTP status', async () => {
  const client = new TodoistClient({}, { resolve: async () => ({ apiToken: new SecretString('inert-token') }) }, {
    send: async () => { throw new BodyTooLargeError(429, { 'retry-after': '1' }); },
  });
  const attempt = await client.attempt(call, 'getTask');
  const answer = await client.request(call, attempt, 'getTask', 'GET', '/tasks/task-123');
  expect(answer.status).toBe(429);
  expect(answer.failure?.kind).toBe('RATE_LIMIT');
  expect(answer.retryAfterMs).toBe(1_000);
});

it('ignores a successful DELETE body as Go does', async () => {
  const client = new TodoistClient({}, { resolve: async () => ({ apiToken: new SecretString('inert-token') }) }, {
    send: async () => { throw new BodyTooLargeError(204); },
  });
  const attempt = await client.attempt(call, 'deleteTask');
  const answer = await client.request(call, attempt, 'deleteTask', 'DELETE', '/tasks/task-123');
  expect(answer.status).toBe(204);
  expect(answer.failure).toBeUndefined();
});
