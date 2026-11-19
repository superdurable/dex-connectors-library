// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../src/project.js';

function project(configuration: unknown): LoadedProject {
  return {
    configuration: { connections: [{ connectorId: 'pagerduty', connectionName: 'pagerduty', provider: 'pagerduty', configuration }] },
    resolveCredential: async () => ({ api_key: 'test-secret-token' }),
  } as unknown as LoadedProject;
}

describe('PagerDuty project connection', () => {
  it('passes a Go duration or nanoseconds to the provider request', async () => {
    for (const [timeout, expectedMilliseconds] of [['1m', 60_000], [1_000_000_000, 1_000]] as const) {
      let requestTimeout = 0;
      const connection = newProjectConnection(project({ timeout }), 'pagerduty', { send: async request => {
        requestTimeout = request.timeoutMs ?? 0;
        return { status: 200, headers: {}, body: Buffer.from('{"incidents":[],"limit":25,"offset":0,"more":false}') };
      } });
      const call = {
        id: 'project-test', connection: connection.reference, operation: { connectorId: 'pagerduty', operationId: 'listIncidents' },
        context: { flowId: 'project-test', cancellationSignal: new AbortController().signal } as Call['context'],
        reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
      } as Call;
      await connection.operations.listIncidents().invoke(call, { limit: 25, offset: 0 });
      expect(requestTimeout).toBe(expectedMilliseconds);
    }
  });

  it('rejects malformed and unknown settings instead of silently dropping them', () => {
    expect(() => newProjectConnection(project({ timeout: 'bogus' }), 'pagerduty')).toThrow();
    expect(() => newProjectConnection(project({ extra: true }), 'pagerduty')).toThrow();
  });
});
