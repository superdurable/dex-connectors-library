// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { SupabaseClient } from '../src/client.js';
import { CredentialMaterialError } from '../src/generated/models.js';

const call = { id: 'test-call', context: { cancellationSignal: new AbortController().signal } } as Call;
const client = new SupabaseClient({ baseUrl: 'http://127.0.0.1:28801/rest/v1' }, { resolve: async () => ({ apiKey: new SecretString('test-token') }) }, {
  send: async () => ({ status: 200, headers: {}, body: Buffer.from('[]') }),
});

describe('credential classification', () => {
  it.each([
    ['reauthorization required', new ReauthorizationRequiredError(), true],
    ['connection object not found', new ObjectNotFoundError(), true],
    ['connection not selected', new ConnectionNotSelectedError(), true],
    ['credential broker rejected', new CredentialBrokerRejectedError(), true],
    ['malformed credential material', new CredentialMaterialError('credential api_key is required'), true],
    ['SDK provider decoding failure', new TypeError('project credential decoding failed'), true],
    ['credential broker unavailable', new CredentialBrokerUnavailableError(), false],
    ['static connection not configured', new Error('connection is not configured'), false],
    ['unlisted error', new TypeError('unlisted'), false],
  ])('classifies a %s credential failure without sending', async (_name, credentialFailure, isTerminal) => {
    let requests = 0;
    const failing = new SupabaseClient({ baseUrl: 'http://127.0.0.1:28801/rest/v1' }, { resolve: async () => { throw credentialFailure; } }, {
      send: async () => { requests++; return { status: 200, headers: {}, body: Buffer.from('[]') }; },
    });
    const expected = isTerminal ? { kind: 'branch', branch: 'defect' } : { kind: 'retry', failure: { kind: 'AVAILABILITY' } };
    expect(await failing.upsertRows().invoke(call, { table: 'projects', onConflict: 'id', rows: [{ id: 1 }] })).toMatchObject(expected);
    expect(await failing.selectRows().invoke(call, { table: 'projects' })).toMatchObject(expected);
    expect(requests).toBe(0);
  });
});

describe('input validation', () => {
  it('rejects unsafe table names before transport', async () => {
    expect(await client.selectRows().invoke(call, { table: 'bad/table' })).toMatchObject({ kind: 'branch', branch: 'defect' });
  });
  it('rejects duplicate conflict keys before transport', async () => {
    expect(await client.upsertRows().invoke(call, { table: 'projects', onConflict: 'id', rows: [{ id: 1 }, { id: 1 }] })).toMatchObject({ kind: 'branch', branch: 'defect' });
  });
});

describe('provider response and query parity', () => {
  const rowsJSON = '[{"id":9007199254740993,"amount":10.50,"scale":1e3}]';
  const credentials = { resolve: async () => ({ apiKey: new SecretString('test-token') }) };

  it('preserves numeric literals in select and upsert outputs', async () => {
    const provider = new SupabaseClient({ baseUrl: 'http://127.0.0.1:28801/rest/v1' }, credentials, {
      send: async request => ({ status: request.method === 'POST' ? 201 : 200, headers: {}, body: Buffer.from(rowsJSON) }),
    });
    const selected = await provider.selectRows().invoke(call, { table: 'projects' });
    const upserted = await provider.upsertRows().invoke(call, { table: 'projects', onConflict: 'id', rows: [{ id: 1 }] });
    expect(selected.kind).toBe('branch');
    expect(upserted.kind).toBe('branch');
    if (selected.kind === 'branch') expect(JSON.stringify(selected.value)).toBe(`{"data":${rowsJSON}}`);
    if (upserted.kind === 'branch') expect(JSON.stringify(upserted.value)).toBe(`{"data":${rowsJSON}}`);
  });

  it('uses Go query ordering and escaping', async () => {
    const urls: string[] = [];
    const provider = new SupabaseClient({ baseUrl: 'http://127.0.0.1:28801/rest/v1' }, credentials, {
      send: async request => { urls.push(request.url); return { status: request.method === 'POST' ? 201 : 200, headers: {}, body: Buffer.from('[]') }; },
    });
    await provider.selectRows().invoke(call, { table: 'projects', offset: 2 });
    await provider.selectRows().invoke(call, { table: 'projects', columns: 'id,name' });
    await provider.upsertRows().invoke(call, { table: 'projects', onConflict: 'id,name', rows: [{ id: 1, name: 'one' }] });
    expect(urls).toEqual([
      'http://127.0.0.1:28801/rest/v1/projects?limit=100&offset=2&select=%2A',
      'http://127.0.0.1:28801/rest/v1/projects?limit=100&select=id%2Cname',
      'http://127.0.0.1:28801/rest/v1/projects?on_conflict=id%2Cname',
    ]);
  });

  it.each([[405, '{}'], [409, '{}'], [201, '{"wrong":true}'], [201, '[']])('retries an upsert answered with HTTP %i body %s', async (status, body) => {
    const answering = new SupabaseClient({ baseUrl: 'http://127.0.0.1:28801/rest/v1' }, { resolve: async () => ({ apiKey: new SecretString('test-token') }) }, {
      send: async () => ({ status, headers: {}, body: Buffer.from(body) }),
    });
    expect(await answering.upsertRows().invoke(call, { table: 'projects', onConflict: 'id', rows: [{ id: 1 }] })).toMatchObject({ kind: 'retry' });
  });
});
