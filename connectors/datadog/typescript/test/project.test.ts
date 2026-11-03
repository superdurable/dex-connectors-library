// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../src/project.js';

function project(configuration: unknown, credentials: unknown = { api_key: 'api', application_key: 'application' }): LoadedProject {
  return {
    configuration: { connections: [{ connectorId: 'datadog', connectionName: 'datadog', provider: 'datadog', configuration }] },
    resolveCredential: async () => credentials,
  } as unknown as LoadedProject;
}

describe('Datadog project connection', () => {
  it('rejects unknown or malformed configuration instead of silently dropping it', () => {
    expect(() => newProjectConnection(project({ timeout: true }), 'datadog')).toThrow();
    expect(() => newProjectConnection(project({ timeout: 'bogus' }), 'datadog')).toThrow();
    expect(() => newProjectConnection(project({ unknownField: 'value' }), 'datadog')).toThrow();
  });

  it('parses a Go duration and validates credentials when used', async () => {
    let requestTimeout = 0;
    const connection = newProjectConnection(project({ timeout: '2s' }), 'datadog', { send: async request => {
      requestTimeout = request.timeoutMs ?? 0;
      return { status: 200, headers: {}, body: Buffer.from('[]') };
    } });
    const call = {
      id: 'project-test', connection: connection.reference, operation: { connectorId: 'datadog', operationId: 'listMonitors' },
      context: { flowId: 'project-test', cancellationSignal: new AbortController().signal } as Call['context'],
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    await connection.operations.listMonitors().invoke(call, { page: 0, pageSize: 1 });
    expect(requestTimeout).toBe(2_000);
  });
});
