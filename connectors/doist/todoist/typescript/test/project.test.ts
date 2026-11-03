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
