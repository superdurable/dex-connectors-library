// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { Client } from '../src/client.js';

const connection = { provider: 'smartsheet', name: 'smartsheet' };
const call = { context: { flowId: 'smartsheet-unit', cancellationSignal: new AbortController().signal } as Call['context'], id: 'unit-call', connection, operation: { connectorId: 'smartsheet', operationId: 'listSheets' }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false } as Call;
const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString('test-secret-key') }]]);

describe('Smartsheet response classification', () => {
  it('uses configured timeout and limits only a successful response', async () => {
    let sent: ProviderRequest | undefined;
    const client = new Client({ baseUrl: 'http://127.0.0.1:28590/2.0', timeoutMs: 7_000, maxResponseBytes: 100 }, credentials, { send: async request => {
      sent = request;
      throw new BodyTooLargeError(429, {});
    } });
    const result = await client.listSheets().invoke(call, {});
    expect(sent?.timeoutMs).toBe(7_000);
    expect(result.kind).toBe('retry');
    if (result.kind === 'retry') expect(result.failure.kind).toBe('RATE_LIMIT');
  });

  it('rejects a wrong-typed provider field', async () => {
    const client = new Client({}, credentials, { send: async () => ({ status: 200, headers: {}, body: Buffer.from('{"pageNumber":1,"totalCount":1,"data":[{"id":"wrong","name":"Sheet"}]}') }) });
    const result = await client.listSheets().invoke(call, {});
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('invalidResponse');
  });

  it('preserves numeric cell tokens and accepts null Go zero values', async () => {
    const body = '{"id":1001,"name":"Sheet","columns":[{"id":null,"title":null}],"rows":[{"id":null,"cells":[{"columnId":1,"value":1.10},{"columnId":1,"value":1e3},{"columnId":1,"value":9007199254740993}]}]}';
    const client = new Client({}, credentials, { send: async () => ({ status: 200, headers: {}, body: Buffer.from(body) }) });
    const result = await client.getSheet().invoke(call, { sheetId: 1001 });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') {
      expect(result.branch).toBe('found');
      expect(result.value.columns).toEqual([{ id: 0, title: '' }]);
      expect(result.value.rows?.[0]?.id).toBe(0);
      expect(JSON.stringify(result.value.rows?.[0]?.cells)).toBe('[{"columnId":1,"value":1.10},{"columnId":1,"value":1e3},{"columnId":1,"value":9007199254740993}]');
    }
  });

  it('accepts null summary fields as Go zero values', async () => {
    const client = new Client({}, credentials, { send: async () => ({ status: 200, headers: {}, body: Buffer.from('{"pageNumber":1,"totalCount":1,"data":[{"id":null,"name":null}]}') }) });
    const result = await client.listSheets().invoke(call, {});
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') {
      expect(result.branch).toBe('listed');
      expect(result.value.sheets).toEqual([{ id: 0, name: '' }]);
    }
  });

  it('treats credential resolution failure as terminal', async () => {
    const client = new Client({}, { resolve: async () => { throw new Error('secret must not leak'); } });
    const result = await client.listSheets().invoke(call, {});
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') { expect(result.branch).toBe('defect'); expect(result.failure?.message).not.toContain('secret must not leak'); }
  });
});
