// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { Client } from '../src/index.js';

const call = { id: 'algolia-test-call', context: { cancellationSignal: new AbortController().signal } } as Call;

describe('Algolia client configuration and credential safety', () => {
  it('rejects invalid local settings at construction', () => {
    const credentials = { resolve: async () => ({ applicationId: 'TESTAPP', apiKey: new SecretString('safe-key') }) };
    expect(() => new Client({ baseUrl: 'http://example.com/1' }, credentials)).toThrow();
    expect(() => new Client({ timeoutMs: -1 }, credentials)).toThrow();
    expect(() => new Client({ maxResponseBytes: -1 }, credentials)).toThrow();
  });

  it('uses the application-specific public host and keeps the key in a header', async () => {
    let observedUrl = '';
    let observedKey = '';
    const client = new Client({}, { resolve: async () => ({ applicationId: 'TESTAPP', apiKey: new SecretString('safe-key') }) }, async request => {
      observedUrl = request.url;
      observedKey = request.headers?.['x-algolia-api-key'] ?? '';
      return { status: 200, headers: {}, body: Buffer.from('{"nbHits":0,"hits":[]}') };
    });
    const result = await client.search().invoke(call, { indexName: 'products', query: '' });
    expect(result.kind).toBe('branch');
    expect(observedUrl).toBe('https://TESTAPP.algolia.net/1/indexes/products/query');
    expect(observedUrl).not.toContain('safe-key');
    expect(observedKey).toBe('safe-key');
  });

  it('does not dispatch when credentials contain a line break', async () => {
    let wasSent = false;
    const client = new Client({}, { resolve: async () => ({ applicationId: 'TESTAPP', apiKey: new SecretString('unsafe\nkey') }) }, async () => {
      wasSent = true;
      throw new Error('unexpected send');
    });
    const result = await client.search().invoke(call, { indexName: 'products', query: '' });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('defect');
    expect(wasSent).toBe(false);
  });
});
