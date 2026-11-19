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
  it('encodes vector coordinates with Go float32 precision for both operations', async () => {
    const requestBodies: string[] = [];
    const client = new PineconeClient({ baseUrl: 'https://example.com' }, credentials, { send: async request => {
      requestBodies.push(Buffer.from(request.body ?? []).toString('utf8'));
      return { status: 200, headers: {}, body: Buffer.from(request.url.endsWith('/query') ? '{"matches":[]}' : '{"upsertedCount":1}') };
    } });
    const values = [0.123456789, 16777217, 0.30000001192092896];
    await client.upsertVectors().invoke(call, { vectors: [{ id: 'one', values }] });
    await client.queryVectors().invoke(call, { vector: values, topK: 1 });
    expect(requestBodies[0]).toContain('"values":[0.12345679,16777216,0.3]');
    expect(requestBodies[1]).toContain('"vector":[0.12345679,16777216,0.3]');
  });
  it('rejects nonfinite and overflowing float32 coordinates before sending', async () => {
    const client = new PineconeClient({ baseUrl: 'https://example.com' }, credentials, { send: async () => { throw new Error('unexpected request'); } });
    for (const value of [NaN, Infinity, 1e100]) {
      const upsert = await client.upsertVectors().invoke(call, { vectors: [{ id: 'one', values: [value] }] });
      const query = await client.queryVectors().invoke(call, { vector: [value], topK: 1 });
      expect(upsert.kind).toBe('branch');
      expect(query.kind).toBe('branch');
      if (upsert.kind === 'branch') expect(upsert.branch).toBe('defect');
      if (query.kind === 'branch') expect(query.branch).toBe('defect');
    }
  });
  it('preserves response number lexemes in query output', async () => {
    const responseBody = '{"matches":[{"score":0.12345678901234567890,"metadata":{"large":12345678901234567890}}]}';
    const client = new PineconeClient({ baseUrl: 'https://example.com' }, credentials, { send: async () => ({ status: 200, headers: {}, body: Buffer.from(responseBody) }) });
    const result = await client.queryVectors().invoke(call, { vector: [1], topK: 1 });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(JSON.stringify(result.value.data)).toBe(responseBody);
  });
  it.each(['http://localhost', 'http://127.0.0.2:5080', 'http://[::1]:5080', 'https://private.example.com'])('allows owner-entered host %s', async baseUrl => {
    let addressPolicy = '';
    const client = new PineconeClient({ baseUrl }, credentials, { send: async request => {
      addressPolicy = request.addressPolicy ?? '';
      return { status: 200, headers: {}, body: Buffer.from('{"matches":[]}') };
    } });
    const result = await client.queryVectors().invoke(call, { vector: [1], topK: 1 });
    expect(result.kind).toBe('branch');
    expect(addressPolicy).toBe('privateNetworkHTTP');
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
