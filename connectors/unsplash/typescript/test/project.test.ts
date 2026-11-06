// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../src/project.js';

function project(configuration: unknown, credentials: unknown = { access_key: 'unsplash-test-key' }): LoadedProject {
  return { configuration: { connections: [{ connectorId: 'unsplash', connectionName: 'unsplash', provider: 'unsplash', configuration }] }, resolveCredential: async () => credentials } as unknown as LoadedProject;
}
it('rejects malformed or unknown connection settings', () => {
  expect(() => newProjectConnection(project({ timeout: true }), 'unsplash')).toThrow();
  expect(() => newProjectConnection(project({ timeout: 'bogus' }), 'unsplash')).toThrow();
  expect(() => newProjectConnection(project({ unknownField: 1 }), 'unsplash')).toThrow();
});
it('maps a Go timeout duration and resolves the current key', async () => {
  let requestTimeout = 0, authorization = '';
  const connection = newProjectConnection(project({ timeout: '2s' }), 'unsplash', { send: async request => {
    requestTimeout = request.timeoutMs ?? 0;
    authorization = request.headers?.Authorization ?? '';
    return { status: 404, headers: {}, body: new Uint8Array() };
  } });
  const call: Call = { id: 'project-test', connection: connection.reference, operation: { connectorId: 'unsplash', operationId: 'getPhoto' }, context: { flowId: 'project-test', cancellationSignal: new AbortController().signal } as Call['context'], reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  await connection.operations.getPhoto().invoke(call, { id: 'office-1' });
  expect(requestTimeout).toBe(2_000);
  expect(authorization).toBe('Client-ID unsplash-test-key');
});
