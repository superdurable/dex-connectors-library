// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ClickUpClient } from '../src/client.js';

it('rejects unsafe configuration and invalid input before sending a provider request', async () => {
  const credentials = { resolve: async () => ({ apiToken: new SecretString('pk_test-token') }) };
  expect(() => new ClickUpClient({ maxResponseBytes: -1 }, credentials)).toThrow();
  expect(() => new ClickUpClient({}, credentials, { apiBaseURL: 'http://clickup.example.com' })).toThrow();
  let requestCount = 0;
  const client = new ClickUpClient({}, credentials, { send: async () => { requestCount++; throw new Error('unexpected request'); } });
  const call = { id: '550e8400-e29b-41d4-a716-446655440000', context: { cancellationSignal: new AbortController().signal } } as Call;
  const result = await client.updateTaskTags().invoke(call, { taskId: '86b2x4k7q', addTags: ['same'], removeTags: ['same'] });
  expect(result.kind).toBe('branch');
  if (result.kind === 'branch') expect(result.branch).toBe('defect');
  expect(requestCount).toBe(0);
});
