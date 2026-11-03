// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { CohereClient } from '../src/client.js';

const call = { context: { cancellationSignal: new AbortController().signal } } as Call;
const validEmbed = { model: 'embed-v4.0', texts: ['hello'], input_type: 'search_query' };

describe('Cohere operation local validation', () => {
  it('selects defect before sending an invalid embedding', async () => {
    let sends = 0;
    const client = new CohereClient({}, { resolve: async () => ({ apiKey: new SecretString('test-secret-key') }) }, {
      send: async () => { sends++; throw new Error('unexpected request'); },
    });
    expect(await client.embed().invoke(call, { ...validEmbed, texts: [' '] })).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'VALIDATION' } });
    expect(sends).toBe(0);
  });

  it('selects terminal authentication defect for unavailable credentials', async () => {
    const client = new CohereClient({}, { resolve: async () => { throw new Error('unavailable'); } });
    expect(await client.embed().invoke(call, validEmbed)).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'AUTHENTICATION' } });
  });

  it('retries local network refusal like the Go client', async () => {
    const client = new CohereClient({}, { resolve: async () => ({ apiKey: new SecretString('test-secret-key') }) }, {
      send: async () => { throw new Error('connection refused'); },
    });
    expect(await client.rerank().invoke(call, { model: 'rerank-v5.0-pro', query: 'release', documents: ['article'] })).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
  });
});
