// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../src/project.js';

function project(configuration: unknown): LoadedProject {
  return {
    configuration: { connections: [{ connectorId: 'todoist', connectionName: 'todoist', provider: 'todoist', configuration }] },
    resolveCredential: async () => ({ api_token: 'inert-todoist-token' }),
  } as unknown as LoadedProject;
}

it('maps the connection timeout and rejects malformed duration values', async () => {
  expect(() => newProjectConnection(project({ timeout: 'bogus' }), 'todoist')).toThrow();
  let requestTimeout = 0;
  const connection = newProjectConnection(project({ timeout: '2s' }), 'todoist', { send: async request => {
    requestTimeout = request.timeoutMs ?? 0;
    return { status: 404, headers: {}, body: Buffer.from('') };
  } });
  const call = {
    id: 'project-test', connection: connection.reference, operation: { connectorId: 'todoist', operationId: 'getTask' },
    context: { flowId: 'project-test', cancellationSignal: new AbortController().signal } as Call['context'],
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  await connection.operations.getTask().invoke(call, { taskId: 'task-123' });
  expect(requestTimeout).toBe(2_000);
});

// Expected milliseconds come from Go time.ParseDuration nanoseconds rounded up; requests cap at 25 seconds.
it.each([
  ['1ns', 1], ['999999ns', 1], ['1000001ns', 2],
  ['1us', 1], ['999.999us', 1], ['999.9999us', 1],
  ['1ms', 1], ['1.000001ms', 2], ['1500us', 2], ['1ms500us', 2],
  ['2.0005s', 2_001], ['.5s', 500], ['5.s', 5_000], ['+5s', 5_000],
  ['1µs', 1], ['1μs', 1], ['0.999999999s', 1_000],
  ['0.000000001s', 1], ['1.23456789s', 1_235],
  ['0.999999999999999999s', 1_000], ['1.0000000000000000001s', 1_000],
  ['0.000000000999999999h', 1],
  ['24.999999s', 25_000], ['24.999999999s', 25_000],
  ['1h2m3.5s', 25_000],
])('maps Go duration %s to %i milliseconds', async (duration, expectedTimeoutMs) => {
  let requestTimeout = 0;
  const connection = newProjectConnection(project({ timeout: duration }), 'todoist', { send: async request => {
    requestTimeout = request.timeoutMs ?? 0;
    return { status: 404, headers: {}, body: Buffer.from('') };
  } });
  const call = {
    id: 'duration-parity', connection: connection.reference, operation: { connectorId: 'todoist', operationId: 'getTask' },
    context: { flowId: 'duration-parity', cancellationSignal: new AbortController().signal } as Call['context'],
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  await connection.operations.getTask().invoke(call, { taskId: 'task-123' });
  expect(requestTimeout).toBe(expectedTimeoutMs);
});

it.each(['0', '0s', '0.1ns', '0.999ns', '0.0000000000000001h', '-5s', '1.2.3s', '1e3s', ' 1s', '9223372036854775808ns'])('rejects unusable Go duration %s', duration => {
  expect(() => newProjectConnection(project({ timeout: duration }), 'todoist')).toThrow();
});
