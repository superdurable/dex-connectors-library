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

it('rejects unsafe tag paths and duplicate tags without sending a request', async () => {
  let requestCount = 0;
  const client = new ClickUpClient({}, { resolve: async () => ({ apiToken: new SecretString('pk_test-token') }) },
    { send: async () => { requestCount++; throw new Error('unexpected request'); } });
  const call = { id: '550e8400-e29b-41d4-a716-446655440000', context: { cancellationSignal: new AbortController().signal } } as Call;
  for (const tag of ['.', '..', ' bad', 'bad ', 'a/b', 'a\0b', 'a\rb', 'a\nb', 'a\tb', '']) {
    const result = await client.updateTaskTags().invoke(call, { taskId: '86b2x4k7q', removeTags: [tag] });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('defect');
  }
  for (const tags of [['same', 'same'], ['same', 'other', 'same']]) {
    const result = await client.updateTaskTags().invoke(call, { taskId: '86b2x4k7q', addTags: tags });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('defect');
  }
  expect(requestCount).toBe(0);
});

it('uses Go path escaping and one cancellation budget for tag writes and read back', async () => {
  const requests: string[] = [];
  const signals: AbortSignal[] = [];
  const client = new ClickUpClient({}, { resolve: async () => ({ apiToken: new SecretString('pk_test-token') }) },
    { send: async request => { requests.push(request.url); signals.push(request.signal!);
      return { status: 200, headers: {}, body: Buffer.from(request.method === 'GET' ? '{"id":"86b2x4k7q","name":"Task","tags":[],"assignees":[]}' : '{}') }; } });
  const originalSignal = new AbortController().signal;
  const call = { id: '550e8400-e29b-41d4-a716-446655440000', context: { cancellationSignal: originalSignal } } as Call;
  const result = await client.updateTaskTags().invoke(call, { taskId: '86b2x4k7q', addTags: ["!$&'()*+:=@"] });
  expect(result.kind).toBe('branch');
  if (result.kind === 'branch') expect(result.branch).toBe('updated');
  expect(requests[0]).toContain('/task/86b2x4k7q/tag/%21$&%27%28%29%2A+:=@');
  expect(signals).toHaveLength(2);
  expect(signals[0]).toBe(signals[1]);
  expect(signals[0]).not.toBe(originalSignal);
});

it('rejects duplicate assignees, invalid statuses, and invalid custom fields before writing', async () => {
  let requestCount = 0;
  const client = new ClickUpClient({}, { resolve: async () => ({ apiToken: new SecretString('pk_test-token') }) },
    { send: async () => { requestCount++; throw new Error('unexpected request'); } });
  const call = { id: '550e8400-e29b-41d4-a716-446655440000', context: { cancellationSignal: new AbortController().signal } } as Call;
  const createInput = { listId: '901410000001', name: 'Task' };
  const invalidCreateInputs = [
    { assigneeIds: [183, 183] }, { status: ' '.repeat(256) }, { status: 'a'.repeat(256) },
    { customFields: [{ id: 'field1', value: 1 }, { id: 'field1', value: 2 }] },
    { customFields: [{ id: 'field1', value: Number.NaN }] },
    { customFields: [{ id: 'field1', value: { nested: undefined } }] },
  ];
  for (const fields of invalidCreateInputs) {
    const result = await client.createTask().invoke(call, { ...createInput, ...fields });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('defect');
  }
  for (const fields of [{ status: ' '.repeat(256) }, { status: 'a'.repeat(256) }, { addAssigneeIds: [183, 183] }, { removeAssigneeIds: [183, 183] }]) {
    const result = await client.updateTask().invoke(call, { taskId: '86b2x4k7q', ...fields });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('defect');
  }
  expect(requestCount).toBe(0);
});
