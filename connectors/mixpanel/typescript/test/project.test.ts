// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../src/project.js';

function project(configuration: unknown): LoadedProject {
  return {
    configuration: { connections: [{ connectorId: 'mixpanel', connectionName: 'mixpanel', provider: 'mixpanel', configuration }] },
    resolveCredential: async () => ({ service_account_username: 'user', service_account_secret: 'secret' }),
  } as unknown as LoadedProject;
}

const call = { context: { cancellationSignal: new AbortController().signal } } as Call;

describe('Mixpanel project connection', () => {
  it.each([
    ['1m', 60_000],
    [60_000_000_000, 60_000],
  ])('passes project timeout %s to the provider request', async (timeout, expectedTimeoutMs) => {
    let requestTimeoutMs = 0;
    let requestUrl = '';
    const connection = newProjectConnection(project({
      baseUrl: 'https://api-eu.mixpanel.com', exportBaseUrl: 'https://data-eu.mixpanel.com/api/2.0',
      projectId: 12345, timeout, maxResponseBytes: 4096,
    }), 'mixpanel', { send: async request => {
      requestTimeoutMs = request.timeoutMs ?? 0;
      requestUrl = request.url;
      return { status: 200, headers: {}, body: Buffer.from('{}') };
    } });
    await connection.operations.exportEvents().invoke(call, { fromDate: '2026-01-01', toDate: '2026-01-01', limit: 1 });
    expect(requestTimeoutMs).toBe(expectedTimeoutMs);
    expect(requestUrl).toContain('https://data-eu.mixpanel.com/api/2.0/export?');
    expect(requestUrl).toContain('project_id=12345');
  });

  it('rejects malformed project settings', () => {
    for (const timeout of [true, 'bogus', '0s', -1]) {
      expect(() => newProjectConnection(project({ projectId: 12345, timeout }), 'mixpanel')).toThrow();
    }
    expect(() => newProjectConnection(project({ projectId: 12345, unknownField: 'value' }), 'mixpanel')).toThrow();
  });
});
