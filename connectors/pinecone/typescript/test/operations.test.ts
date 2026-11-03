// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { PineconeClient } from '../src/client.js';

const call = { id: 'pinecone-unit-call', context: { cancellationSignal: new AbortController().signal } } as Call;
const credentials = { resolve: async () => ({ apiKey: new SecretString('safe-test-key') }) };

describe('Pinecone input and configuration', () => {
  it('rejects missing credentials and unsafe configuration', () => {
    expect(() => new PineconeClient({ baseUrl: 'http://example.com' }, credentials)).toThrow();
    expect(() => new PineconeClient({ baseUrl: 'https://example.com', maxResponseBytes: -1 }, credentials)).toThrow();
  });
  it('rejects duplicate upsert IDs before any request', async () => {
    const client = new PineconeClient({ baseUrl: 'https://example.com' }, credentials, { send: async () => { throw new Error('unexpected request'); } });
    const result = await client.upsertVectors().invoke(call, { vectors: [{ id: 'same', values: [1] }, { id: 'same', values: [2] }] });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('defect');
  });
  it('classifies unavailable credentials as terminal authentication', async () => {
    const client = new PineconeClient({ baseUrl: 'https://example.com' }, { resolve: async () => { throw new Error('secret sentinel'); } });
    const result = await client.queryVectors().invoke(call, { vector: [1], topK: 1 });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('defect');
    expect(result.failure?.kind).toBe('AUTHENTICATION');
    expect(result.failure?.message).not.toContain('sentinel');
  });
  it('omits Go empty fields from the provider JSON body', async () => {
    let encoded = '';
    const client = new PineconeClient({ baseUrl: 'https://example.com' }, credentials, { send: async request => {
      encoded = Buffer.from(request.body ?? []).toString('utf8');
      return { status: 200, headers: {}, body: Buffer.from('{"upsertedCount":1}') };
    } });
    await client.upsertVectors().invoke(call, { namespace: '', vectors: [{ id: 'one', values: [1], metadata: {} }] });
    expect(JSON.parse(encoded)).toEqual({ vectors: [{ id: 'one', values: [1] }] });
  });
  it('retries a rate limit even when its response body exceeds the limit', async () => {
    const client = new PineconeClient({ baseUrl: 'https://example.com' }, credentials, { send: async () => {
      throw new BodyTooLargeError(429, {});
    } });
    const result = await client.queryVectors().invoke(call, { vector: [1], topK: 1 });
    expect(result.kind).toBe('retry');
    expect(result.failure?.kind).toBe('RATE_LIMIT');
  });
});
