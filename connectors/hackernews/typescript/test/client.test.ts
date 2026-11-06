// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { HackerNewsClient } from '../src/client.js';
import type { Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { newProjectConnection } from '../src/index.js';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';

const call = { context: { cancellationSignal: new AbortController().signal } } as Call;

describe('HackerNewsClient', () => {
  it('rejects invalid configuration before a provider request', () => {
    expect(() => new HackerNewsClient({ timeoutMs: -1 })).toThrow();
    expect(() => new HackerNewsClient({ maxResponseBytes: -1 })).toThrow();
    expect(() => new HackerNewsClient({ timeoutMs: Number.NaN })).toThrow();
    expect(() => new HackerNewsClient({ maxResponseBytes: Number.NaN })).toThrow();
  });

  it('reads the manifest timeout from the project connection', () => {
    const project = { configuration: { connections: [{
      connectorId: 'hacker-news-daily', connectionName: 'public', provider: 'hacker-news',
      configuration: { timeout: -1_000_000 },
    }] } } as unknown as LoadedProject;
    expect(() => newProjectConnection(project, 'public')).toThrow('timeout');
  });

  it('keeps invalid feed input local', async () => {
    let requests = 0;
    const client = new HackerNewsClient({}, { send: async () => { requests++; throw new Error('unexpected request'); } });
    const result = await client.listStoryIDs().invoke(call, { feed: '../item', limit: 1 });
    expect(result).toMatchObject({ branch: 'defect', failure: { kind: 'VALIDATION' } });
    expect(requests).toBe(0);
  });

  it('classifies an oversized 429 by status before body size', async () => {
    const client = new HackerNewsClient({ maxResponseBytes: 10 }, {
      now: () => new Date('2026-10-26T12:00:00Z'),
      send: async () => { throw new BodyTooLargeError(429, { 'retry-after': '2' }); },
    });
    const result = await client.getItem().invoke(call, { id: 42001 });
    expect(result).toMatchObject({ kind: 'retry', failure: { kind: 'RATE_LIMIT' }, retryAfterMs: 2000 });
  });

  it('names an interrupted 200 response', async () => {
    const client = new HackerNewsClient({}, { send: async () => { throw new ProviderTransportError(false, 200); } });
    expect(await client.getItem().invoke(call, { id: 42001 })).toMatchObject({
      kind: 'retry', failure: { kind: 'AVAILABILITY', message: 'Hacker News response was interrupted' },
    });
  });

  for (const score of ['5.0', '1e2', '9007199254740993']) {
    it(`rejects noncanonical score ${score} and names the field`, async () => {
      const client = new HackerNewsClient({}, { send: async () => ({
        status: 200, headers: {}, body: Buffer.from(`{"id":42001,"type":"story","score":${score}}`),
      }) });
      const result = await client.getItem().invoke(call, { id: 42001 });
      expect(result).toMatchObject({ branch: 'invalidResponse', failure: { kind: 'PROTOCOL', message: expect.stringContaining('score') } });
    });
  }

  for (const id of ['5.0', '1e2', '9007199254740993']) {
    it(`rejects noncanonical feed ID ${id} and names the field`, async () => {
      const client = new HackerNewsClient({}, { send: async () => ({ status: 200, headers: {}, body: Buffer.from(`[${id}]`) }) });
      const result = await client.listStoryIDs().invoke(call, { feed: 'top', limit: 1 });
      expect(result).toMatchObject({ branch: 'invalidResponse', failure: { kind: 'PROTOCOL', message: expect.stringContaining('item ID') } });
    });
  }

  it('rejects a wrong-typed provider field', async () => {
    const client = new HackerNewsClient({}, { send: async () => ({
      status: 200, headers: {}, body: Buffer.from('{"id":42001,"type":"story","score":"5"}'),
    }) });
    const result = await client.getItem().invoke(call, { id: 42001 });
    expect(result).toMatchObject({ branch: 'invalidResponse', failure: { kind: 'PROTOCOL', message: expect.stringContaining('score') } });
  });

  it('classifies null feed and child IDs with Go messages', async () => {
    const feed = new HackerNewsClient({}, { send: async () => ({ status: 200, headers: {}, body: Buffer.from('[null]') }) });
    expect(await feed.listStoryIDs().invoke(call, { feed: 'top', limit: 1 })).toMatchObject({ failure: { message: 'Hacker News returned an invalid item ID' } });
    const item = new HackerNewsClient({}, { send: async () => ({ status: 200, headers: {}, body: Buffer.from('{"id":42001,"type":"story","kids":[null]}') }) });
    expect(await item.getItem().invoke(call, { id: 42001 })).toMatchObject({ failure: { message: 'Hacker News returned an invalid child ID' } });
  });
});
