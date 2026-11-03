// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { Client, newConnection, newProjectConnection, type Credentials } from '../src/index.js';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';

const connection = { provider: 'alphavantage', name: 'alphavantage' };
const call: Call = {
  id: '00000000-0000-5000-8000-000000000001', connection,
  operation: { connectorId: 'alphavantage', operationId: 'getGlobalQuote' },
  context: { flowId: 'alphavantage-test', cancellationSignal: new AbortController().signal } as Call['context'],
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
};

describe('Alpha Vantage provider client', () => {
  it('sends a bounded quote request and returns only safe provider values', async () => {
    const requests: ProviderRequest[] = [];
    const client = new Client({ baseUrl: 'http://127.0.0.1:21101', maxResponseBytes: 1024 },
      new StaticCredentialProvider([[connection, { apiKey: new SecretString('placeholder-key') }]]),
      async request => {
        requests.push(request);
        return { status: 200, headers: {}, body: Buffer.from('{"Global Quote":{"01. symbol":"IBM","05. price":"255.25","07. latest trading day":"2026-10-28"}}') };
      });
    const result = await client.getGlobalQuote().invoke(call, { symbol: 'IBM' });
    expect(result).toMatchObject({ kind: 'branch', branch: 'found', value: { price: '255.25' } });
    expect(new URL(requests[0]?.url ?? '').searchParams.get('apikey')).toBe('placeholder-key');
    expect(requests[0]).toMatchObject({ method: 'GET', maxResponseBytes: 1024, timeoutMs: 15000, headers: { Accept: 'application/json' } });
    expect(JSON.stringify(result)).not.toContain('placeholder-key');
  });

  it('stops invalid symbols and missing credentials before sending', async () => {
    let requests = 0;
    const client = new Client({}, new StaticCredentialProvider([]), async () => { requests++; throw new Error('unexpected request'); });
    expect(await client.getGlobalQuote().invoke(call, { symbol: '/IBM' })).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'VALIDATION' } });
    expect(await client.getGlobalQuote().invoke(call, { symbol: 'IBM' })).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'AUTHENTICATION' } });
    expect(requests).toBe(0);
  });

  it('treats JSON null as absent market data', async () => {
    const client = new Client({ baseUrl: 'http://127.0.0.1:21101' },
      new StaticCredentialProvider([[connection, { apiKey: new SecretString('placeholder-key') }]]),
      async () => ({ status: 200, headers: {}, body: Buffer.from('null') }));
    expect(await client.getGlobalQuote().invoke(call, { symbol: 'IBM' })).toMatchObject({ kind: 'branch', branch: 'notFound' });
    expect(await client.getTimeSeriesDaily().invoke(call, { symbol: 'IBM' })).toMatchObject({ kind: 'branch', branch: 'notFound' });
  });

  it('validates configuration and named connections', () => {
    const credentials = new StaticCredentialProvider<Credentials>([]);
    expect(() => new Client({ baseUrl: 'http://example.com' }, credentials)).toThrow();
    expect(() => new Client({ timeoutMs: -1 }, credentials)).toThrow();
    expect(() => new Client({ maxResponseBytes: 0 }, credentials)).toThrow();
    expect(() => newConnection(new Client({}, credentials), { provider: 'other', name: 'alphavantage' })).toThrow();
  });

  it('loads Dex Web configuration without retaining the API key', async () => {
    const project = {
      configuration: { connections: [{ connectorId: 'alphavantage', provider: 'alphavantage', connectionName: 'alphavantage', configuration: { baseUrl: 'https://www.alphavantage.co', timeout: '20s', maxResponseBytes: 2048 } }] },
      async resolveCredential() { return { api_key: 'placeholder-key' }; },
    } as unknown as LoadedProject;
    const configured = newProjectConnection(project, 'alphavantage');
    expect(configured.reference).toEqual(connection);
    expect(configured.operations.getGlobalQuote().definition().operation.operationId).toBe('getGlobalQuote');
  });
});
