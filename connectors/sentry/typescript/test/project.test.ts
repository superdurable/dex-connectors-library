// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../src/project.js';

function project(configuration: unknown): LoadedProject {
  return {
    configuration: { connections: [{ connectorId: 'sentry', connectionName: 'sentry-issue-review', provider: 'sentry', configuration }] },
    resolveCredential: async () => ({ api_key: 'test-secret-token' }),
  } as unknown as LoadedProject;
}

describe('Sentry project connection', () => {
  it('uses the configured Go duration and rejects invalid settings', async () => {
    let requestTimeout = 0;
    const connection = newProjectConnection(project({ baseUrl: 'http://127.0.0.1:12345', timeout: '45s' }), 'sentry-issue-review', {
      send: async request => {
        requestTimeout = request.timeoutMs ?? 0;
        return { status: 200, headers: {}, body: Buffer.from('[]') };
      },
    });
    const call = {
      id: 'project-test', connection: connection.reference, operation: { connectorId: 'sentry', operationId: 'listIssues' },
      context: { flowId: 'project-test', cancellationSignal: new AbortController().signal } as Call['context'],
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    await connection.operations.listIssues().invoke(call, { organization: 'acme', query: '' });
    expect(requestTimeout).toBe(45_000);
    const numericConnection = newProjectConnection(project({ baseUrl: 'http://127.0.0.1:12345', timeout: 45_000_000_000 }), 'sentry-issue-review', {
      send: async request => {
        requestTimeout = request.timeoutMs ?? 0;
        return { status: 200, headers: {}, body: Buffer.from('[]') };
      },
    });
    await numericConnection.operations.listIssues().invoke(call, { organization: 'acme', query: '' });
    expect(requestTimeout).toBe(45_000);
    expect(() => newProjectConnection(project({ timeout: 'bogus' }), 'sentry-issue-review')).toThrow();
    expect(() => newProjectConnection(project({ unknownField: 'value' }), 'sentry-issue-review')).toThrow();
  });
});
