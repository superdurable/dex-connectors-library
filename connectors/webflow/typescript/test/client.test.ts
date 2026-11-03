// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { WebflowClient } from '../src/index.js';

const connection = { provider: 'webflow', name: 'webflow' };
const credentials = new StaticCredentialProvider([[connection, { siteToken: new SecretString('test-token') }]]);
const call = { id: 'unit-call', connection, operation: { connectorId: 'webflow', operationId: 'listCollectionItems' }, context: { cancellationSignal: new AbortController().signal } } as Call;

describe('Webflow client configuration', () => {
  it('passes the configured timeout and size limit to the HTTP sender', async () => {
    let timeoutMs = 0;
    let maxResponseBytes = 0;
    const client = new WebflowClient({ baseUrl: 'http://127.0.0.1:30403/v2', timeoutMs: 2300, maxResponseBytes: 100_000 }, credentials, {
      send: async request => {
        timeoutMs = request.timeoutMs ?? 0;
        maxResponseBytes = request.maxResponseBytes;
        return { status: 200, headers: {}, body: Buffer.from('{"items":[]}') };
      },
    });
    const result = await client.listCollectionItems().invoke(call, { collectionId: '62b720ef280c7a7a3be8cabe' });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('listed');
    expect(timeoutMs).toBe(2300);
    expect(maxResponseBytes).toBe(100_000);
  });

  it('rejects invalid settings before a provider call', () => {
    expect(() => new WebflowClient({ timeoutMs: -1 }, credentials)).toThrow();
    expect(() => new WebflowClient({ maxResponseBytes: -1 }, credentials)).toThrow();
  });
});
