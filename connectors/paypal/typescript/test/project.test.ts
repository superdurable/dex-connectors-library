// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../src/project.js';

function project(configuration: unknown): LoadedProject {
  return {
    configuration: { connections: [{ connectorId: 'paypal', connectionName: 'paypal', provider: 'paypal', configuration }] },
    resolveCredential: async () => ({ client_id: 'client-test', client_secret: 'secret-test' }),
  } as unknown as LoadedProject;
}

it.each([{ timeout: '2s', expected: 2_000 }, { timeout: '1m', expected: 60_000 }, { timeout: 2_000_000_000, expected: 2_000 }])(
  'applies project timeout $timeout to both provider requests', async ({ timeout, expected }) => {
    const requestTimeouts: number[] = [];
    const connection = newProjectConnection(project({ baseUrl: 'http://127.0.0.1:12345', timeout }), 'paypal', { send: async request => {
      requestTimeouts.push(request.timeoutMs ?? 0);
      return request.url.endsWith('/v1/oauth2/token')
        ? { status: 200, headers: {}, body: Buffer.from('{"access_token":"token","token_type":"Bearer"}') }
        : { status: 200, headers: {}, body: Buffer.from('{"id":"ORDER101"}') };
    } });
    const call = { id: 'project-test', connection: connection.reference, operation: { connectorId: 'paypal', operationId: 'getOrder' },
      context: { flowId: 'project-test', cancellationSignal: new AbortController().signal } } as Call;
    await connection.operations.getOrder().invoke(call, { orderId: 'ORDER101' });
    expect(requestTimeouts).toEqual([expected, expected]);
  },
);

it('rejects unknown and invalid project settings', () => {
  expect(() => newProjectConnection(project({ timeout: 'invalid' }), 'paypal')).toThrow();
  expect(() => newProjectConnection(project({ unexpected: true }), 'paypal')).toThrow();
});
