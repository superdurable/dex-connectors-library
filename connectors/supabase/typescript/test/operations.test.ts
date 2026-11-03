// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { SupabaseClient } from '../src/client.js';

const call = { id: 'test-call', context: { cancellationSignal: new AbortController().signal } } as Call;
const client = new SupabaseClient({ baseUrl: 'http://127.0.0.1:28801/rest/v1' }, { resolve: async () => ({ apiKey: new SecretString('test-token') }) }, {
  send: async () => ({ status: 200, headers: {}, body: Buffer.from('[]') }),
});

describe('input validation', () => {
  it('rejects unsafe table names before transport', async () => {
    expect(await client.selectRows().invoke(call, { table: 'bad/table' })).toMatchObject({ kind: 'branch', branch: 'defect' });
  });
  it('rejects duplicate conflict keys before transport', async () => {
    expect(await client.upsertRows().invoke(call, { table: 'projects', onConflict: 'id', rows: [{ id: 1 }, { id: 1 }] })).toMatchObject({ kind: 'branch', branch: 'defect' });
  });
});
