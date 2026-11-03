// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { Client } from '../src/client.js';
import { customFieldValues, decodeTask } from '../src/task.js';

function call(operationId: string): Call {
  let heartbeat: unknown;
  return {
    id: '6072a0ce-9ddb-5937-8998-675e48cfa406',
    connection: { provider: 'asana', name: 'test' }, operation: { connectorId: 'asana', operationId },
    context: {
      flowId: 'unit', stepExecutionId: 'step', attempt: 1, deadline: new Date(Date.now() + 30_000), cancellationSignal: new AbortController().signal,
      hasLastHeartbeatValue: () => heartbeat !== undefined, getLastHeartbeatValue: () => heartbeat,
      recordHeartbeat: async (value: unknown) => { heartbeat = value; },
    } as Call['context'],
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
}

it('rejects duplicate custom fields before a provider request', async () => {
  let requests = 0;
  const client = new Client({}, { resolve: async () => ({ accessToken: new SecretString('inert-token') }) }, {
    send: async () => { requests++; throw new Error('must not send'); },
  });
  const result = await client.createTask().invoke(call('createTask'), {
    name: 'Approved request', projectId: '1201000000000001', customFields: [
      { customFieldId: '1300000000000001', text: 'one' }, { customFieldId: '1300000000000001', text: 'two' },
    ],
  });
  expect(result.kind).toBe('branch');
  if (result.kind === 'branch') expect([result.branch, result.failure?.kind]).toEqual(['defect', 'VALIDATION']);
  expect(requests).toBe(0);
});

it('preserves a zero numeric custom field and rejects nonfinite values', () => {
  expect(customFieldValues([{ customFieldId: '1300000000000001', number: 0 }])).toEqual({ '1300000000000001': 0 });
  expect(() => customFieldValues([{ customFieldId: '1300000000000001', number: Infinity }])).toThrow('finite');
});

it('bounds notes by Unicode characters while decoding task details', () => {
  const task = decodeTask({ gid: '1204567890123456', notes: '🙂'.repeat(65_537), created_at: '2026-09-30T16:15:00Z', modified_at: '2026-09-30T16:20:00Z' }, true);
  expect([...task.notes!]).toHaveLength(65_536);
  expect(task.isNotesTruncated).toBe(true);
  expect(() => decodeTask({ gid: '1204567890123456', created_at: '2026-02-30T16:15:00Z' }, false)).toThrow('ISO 8601');
});
