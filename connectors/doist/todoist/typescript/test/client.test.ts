// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { TodoistClient } from '../src/client.js';
import { CredentialMaterialError } from '../src/generated/models.js';
import { AttemptContext } from './attempt-context.js';

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

it.each([
  ['malformed stored material', () => new CredentialMaterialError('stored credential does not match the connector credential fields'), false],
  ['SDK decoding failure', () => new TypeError('project credential decoding failed'), false],
  ['unlisted type error', () => new TypeError('unlisted'), true],
])('classifies a %s credential failure', async (_name, newError, isTransient) => {
  const client = new TodoistClient({}, { resolve: async () => { throw newError(); } }, {
    send: async () => { throw new Error('no request is expected'); },
  });
  const attempt = await client.attempt(call, 'getTask');
  expect(attempt.token).toBeUndefined();
  expect(attempt.failure?.isTransient).toBe(isTransient);
  expect(attempt.failure?.failure?.kind).toBe(isTransient ? 'AVAILABILITY' : 'AUTHENTICATION');
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

// Go strings.TrimSpace yields blank only for the first two entries.
it.each([
  ['\u0085', true], [' \u0085 ', true], ['\ufeff', false], [' \ufeff ', false],
  ['\u200b', false], ['\u00a0', true], ['\u180e', false],
])('matches Go task-content whitespace for %j', async (content, isBlank) => {
  let requestCount = 0;
  const client = new TodoistClient({}, { resolve: async () => ({ apiToken: new SecretString('inert-token') }) }, {
    send: async () => { requestCount++; return { status: 400, headers: {}, body: Buffer.from('') }; },
  });
  const mutationCall = { ...call, id: '3f1d6a52-0c3e-5b8e-9a41-2f7d6c1b9e04', context: new AttemptContext(), idempotencyKey: '12345678-1234-1234-1234-123456789abc' } as unknown as Call;
  const update = await client.updateTaskContent().invoke(mutationCall, { taskId: 'task-123', content });
  const create = await client.createTask().invoke(mutationCall, { content });
  expect(update.kind).toBe('branch');
  expect(create.kind).toBe('branch');
  if (update.kind === 'branch') expect(update.branch).toBe(isBlank ? 'defect' : 'providerRejected');
  if (create.kind === 'branch') expect(create.branch).toBe(isBlank ? 'defect' : 'providerRejected');
  expect(requestCount).toBe(isBlank ? 0 : 2);
});
