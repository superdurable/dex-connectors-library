// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { DatadogClient } from '../src/client.js';

const connection = { provider: 'datadog', name: 'datadog' };
const call = {
  id: 'test-call', connection, operation: { connectorId: 'datadog', operationId: 'queryMetrics' },
  context: { flowId: 'test-flow', cancellationSignal: new AbortController().signal } as Call['context'],
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
};
const credentials = { resolve: async () => ({ apiKey: new SecretString('api-key'), applicationKey: new SecretString('application-key') }) };

describe('Datadog provider classification', () => {
  it('rejects an invalid metric window without sending a request', async () => {
    let sends = 0;
    const client = new DatadogClient({}, credentials, { send: async () => { sends++; throw new Error('unexpected request'); } });
    const attempt = await client.queryMetrics().invoke(call, { from: 0, to: 1, query: 'system.cpu.user{*}' });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind !== 'branch') throw new Error('expected a terminal branch');
    expect(attempt.branch).toBe('defect');
    expect(attempt.failure?.kind).toBe('VALIDATION');
    expect(sends).toBe(0);
  });

  it('keeps unavailable credentials terminal and secret safe', async () => {
    const client = new DatadogClient({}, { resolve: async () => { throw new Error('secret sentinel'); } });
    const attempt = await client.listMonitors().invoke(call, { page: 0, pageSize: 1 });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind !== 'branch') throw new Error('expected a terminal branch');
    expect(attempt.branch).toBe('defect');
    expect(attempt.failure?.kind).toBe('AUTHENTICATION');
    expect(JSON.stringify(attempt)).not.toContain('secret sentinel');
  });

  it('retries local transport refusal like Go', async () => {
    const client = new DatadogClient({}, credentials, { send: async () => { throw new Error('ECONNREFUSED'); } });
    const attempt = await client.listMonitors().invoke(call, { page: 0, pageSize: 1 });
    expect(attempt.kind).toBe('retry');
    expect(attempt.failure?.kind).toBe('AVAILABILITY');
  });
});
