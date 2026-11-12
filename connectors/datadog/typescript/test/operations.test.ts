// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { DatadogClient } from '../src/client.js';
import { ListMonitorsInputSchema, type QueryMetricsInput } from '../src/generated/models.js';

const connection = { provider: 'datadog', name: 'datadog' };
const call = {
  id: 'test-call', connection, operation: { connectorId: 'datadog', operationId: 'queryMetrics' },
  context: { flowId: 'test-flow', cancellationSignal: new AbortController().signal } as Call['context'],
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
};
const credentials = { resolve: async () => ({ apiKey: new SecretString('api-key'), applicationKey: new SecretString('application-key') }) };

describe('Datadog provider classification', () => {
  it('accepts monitor input that omits the defaulted page fields', () => {
    expect(ListMonitorsInputSchema.parse({})).toEqual({});
    expect(ListMonitorsInputSchema.parse({ page: 2 })).toEqual({ page: 2 });
  });

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

  it('treats a missing query as a validation failure', async () => {
    const client = new DatadogClient({}, credentials, { send: async () => { throw new Error('unexpected request'); } });
    const attempt = await client.queryMetrics().invoke(call, { from: 1, to: 2 } as QueryMetricsInput);
    expect(attempt.kind).toBe('branch');
    if (attempt.kind !== 'branch') throw new Error('expected a terminal branch');
    expect(attempt.branch).toBe('defect');
    expect(attempt.failure?.kind).toBe('VALIDATION');
  });

  it('encodes query values like Go url.Values.Encode', async () => {
    let requestURL = '';
    const client = new DatadogClient({}, credentials, { send: async request => {
      requestURL = request.url;
      return { status: 200, headers: {}, body: Buffer.from('{"status":"ok","series":[]}') };
    } });
    await client.queryMetrics().invoke(call, { from: 1, to: 2, query: "a b!*'()~+" });
    expect(requestURL).toBe('https://api.datadoghq.com/api/v1/query?from=1&query=a+b%21%2A%27%28%29~%2B&to=2');
  });

  it('accepts a representable monitor ID above the safe integer limit', async () => {
    const client = new DatadogClient({}, credentials, { send: async () => ({
      status: 200, headers: {}, body: Buffer.from('[{"id":9007199254740994,"name":"high","type":"metric alert","overall_state":"OK"}]'),
    }) });
    const attempt = await client.listMonitors().invoke(call, { page: 0, pageSize: 1 });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind !== 'branch') throw new Error('expected a terminal branch');
    expect(attempt.branch).toBe('listed');
    expect(attempt.value.monitors?.[0]?.id).toBe(9_007_199_254_740_994);
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
