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

  it('treats credential resolution failure as terminal', async () => {
    const client = new Client({}, { resolve: async () => { throw new Error('secret must not leak'); } });
    const result = await client.listSheets().invoke(call, {});
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') { expect(result.branch).toBe('defect'); expect(result.failure?.message).not.toContain('secret must not leak'); }
  });
});
