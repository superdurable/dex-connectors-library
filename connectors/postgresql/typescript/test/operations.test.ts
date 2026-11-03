// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import pg, { type Client as PgClient } from 'pg';
import Cursor from 'pg-cursor';
import { describe, expect, it, vi } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { PostgreSQLClient } from '../src/client.js';

const call = { id: '00000000-0000-4000-8000-000000000001', idempotencyKey: '00000000-0000-4000-8000-000000000001' } as Call;
const configuration = { host: '127.0.0.1', database: 'app', user: 'dex_app', sslMode: 'disable' };
const columns = [{ name: 'id', dataTypeID: 20 }, { name: 'amount', dataTypeID: 1700 }];

function fakeClient(statementResult: { rows?: unknown[][]; fields?: typeof columns; rowCount?: number; command?: string | null; commandTag?: string } = {}, errors: Record<string, unknown> = {}) {
  const queries: Array<{ text: string; values?: unknown[] }> = [];
  const driver = {
    processID: 1001,
    on: vi.fn(),
    connect: vi.fn(async () => { if (errors.connect) throw errors.connect; }),
    end: vi.fn(async () => undefined),
    query: vi.fn((request: string | { text: string; values?: unknown[] }) => {
      const text = typeof request === 'string' ? request : request.text;
      queries.push({ text, ...(typeof request === 'string' ? {} : { values: request.values }) });
      if (request instanceof Cursor) {
        let offset = 0;
        let isDone = false;
        const sourceRows = statementResult.rows ?? [['9007199254740993', '1200.00']];
        Object.assign(request, {
          handleCommandComplete: () => undefined,
          read: (count: number, callback: (error: Error | undefined, rows: unknown[][], result: unknown) => void) => {
            if (errors[text]) { callback(errors[text] as Error, [], undefined); return; }
            if (isDone) { callback(undefined, [], undefined); return; }
            const rows = sourceRows.slice(offset, offset + count);
            offset += rows.length;
            if (offset === sourceRows.length && rows.length < count) {
              isDone = true;
              if (statementResult.command !== null) (request as Cursor<unknown[]> & { handleCommandComplete?: (message: { text: string }) => void }).handleCommandComplete?.({ text: statementResult.commandTag ?? `${statementResult.command ?? 'SELECT'} ${statementResult.rowCount ?? sourceRows.length}` });
            }
            callback(undefined, rows, { command: isDone ? (statementResult.command === undefined ? 'SELECT' : statementResult.command) : null, fields: statementResult.fields ?? columns, rowCount: isDone ? (statementResult.rowCount ?? sourceRows.length) : null });
          },
          close: async () => undefined,
        });
        return request;
      }
      if (errors[text]) return Promise.reject(errors[text]);
      return Promise.resolve({ command: text === 'COMMIT' ? 'COMMIT' : 'SET', fields: [], rows: [], rowCount: 0 });
    }),
  };
  const client = new PostgreSQLClient(configuration, { resolve: async () => ({ password: new SecretString('private-password') }) }, { createDriver: () => driver as unknown as PgClient, now: () => new Date('2026-09-30T12:00:00Z') });
  return { client, driver, queries };
}

describe('PostgreSQL operations', () => {
  it('runs one read-only transaction and preserves exact numeric text', async () => {
    const { client, queries, driver } = fakeClient();
    const result = await client.query().invoke(call, { statement: 'SELECT id, amount FROM ledger WHERE id = $1', parameters: ['42'] });
    expect(result).toMatchObject({ kind: 'branch', branch: 'completed', value: { columns: [{ name: 'id', typeName: 'int8', typeOid: 20 }, { name: 'amount', typeName: 'numeric', typeOid: 1700 }], rows: [{ id: '9007199254740993', amount: '1200.00' }] }, receipt: { metadata: { backendPid: '1001' } } });
    expect(queries[0]?.text).toBe('begin transaction read only');
    expect(queries.some(query => query.text === 'COMMIT')).toBe(false);
    expect(queries.at(-1)?.values).toEqual(['42']);
    expect(JSON.stringify(result)).not.toContain('private-password');
    expect(driver.end).toHaveBeenCalledOnce();
  });

  it('keeps cursor metadata after completion and decodes a nonempty RETURNING result', async () => {
    const { client } = fakeClient({ command: 'INSERT', commandTag: 'INSERT 0 1', rowCount: 1 });
    expect(await client.execute().invoke(call, { statement: 'INSERT INTO ledger VALUES (1) RETURNING id, amount' })).toMatchObject({ kind: 'branch', branch: 'completed', value: { command: 'INSERT', rowsAffected: 1, rows: [{ id: '9007199254740993', amount: '1200.00' }] } });
  });

  it('keeps the last result when a completed cursor returns an empty batch without metadata', async () => {
    const rows = Array.from({ length: 100 }, (_, index) => [String(index)]);
    const { client } = fakeClient({ fields: [{ name: 'id', dataTypeID: 20 }], rows, rowCount: 100 });
    const result = await client.query().invoke(call, { statement: 'SELECT id FROM ledger' });
    expect(result).toMatchObject({ kind: 'branch', branch: 'completed', value: { rows: expect.arrayContaining([{ id: '99' }]) } });
    if (result.kind === 'branch') expect(result.value.rows).toHaveLength(100);
  });

  it('preserves a multiword command tag', async () => {
    const { client } = fakeClient({ rows: [], fields: [], command: 'CREATE', commandTag: 'CREATE TABLE', rowCount: 0 });
    expect(await client.execute().invoke(call, { statement: 'CREATE TABLE sample (id integer)' })).toMatchObject({ kind: 'branch', branch: 'completed', value: { command: 'CREATE TABLE' } });
  });

  it('keeps microsecond timestamptz text and rejects lossy JSON by column', async () => {
    const timestamp = fakeClient({ fields: [{ name: 'moment', dataTypeID: 1184 }], rows: [['2024-01-01 00:00:00.123457+00']] });
    expect(await timestamp.client.query().invoke(call, { statement: 'SELECT now() AS moment' })).toMatchObject({ kind: 'branch', branch: 'completed', value: { rows: [{ moment: '2024-01-01T00:00:00.123457Z' }] } });
    const offset = fakeClient({ fields: [{ name: 'moment', dataTypeID: 1184 }], rows: [['2024-01-01 02:30:00.500000+02:30']] });
    expect(await offset.client.query().invoke(call, { statement: 'SELECT now() AS moment' })).toMatchObject({ kind: 'branch', branch: 'completed', value: { rows: [{ moment: '2024-01-01T00:00:00.5Z' }] } });
    const priorDay = fakeClient({ fields: [{ name: 'moment', dataTypeID: 1184 }], rows: [['2024-03-01 00:30:00+02']] });
    expect(await priorDay.client.query().invoke(call, { statement: 'SELECT now() AS moment' })).toMatchObject({ kind: 'branch', branch: 'completed', value: { rows: [{ moment: '2024-02-29T22:30:00Z' }] } });
    for (const document of ['{"a":1.10}', '{"b":12345678901234567890.123456789}', '{"c":1e400}']) {
      const invalid = fakeClient({ fields: [{ name: 'document', dataTypeID: 3802 }], rows: [[document]] });
      expect(await invalid.client.query().invoke(call, { statement: 'SELECT document' })).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'PROTOCOL', message: expect.stringContaining('document') } });
    }
  });

  it('counts placeholders after escaped quotes in E strings', async () => {
    const { client, queries } = fakeClient();
    expect(await client.query().invoke(call, { statement: "SELECT E'a\\'b', $1", parameters: [1] })).toMatchObject({ kind: 'branch', branch: 'completed' });
    expect(queries.at(-1)?.values).toEqual(['1']);
  });

  it('routes row and byte bounds to truncated without committing', async () => {
    const { driver } = fakeClient({ rows: [['1', '1.00'], ['2', '2.00'], ['3', '3.00']], rowCount: 3 });
    const bounded = new PostgreSQLClient({ ...configuration, maxRows: 1 }, { resolve: async () => ({ password: new SecretString('p') }) }, { createDriver: () => driver as unknown as PgClient });
    const result = await bounded.query().invoke(call, { statement: 'SELECT id, amount FROM ledger' });
    expect(result).toMatchObject({ kind: 'branch', branch: 'truncated', value: { truncated: true }, failure: { kind: 'RESPONSE_TOO_LARGE' } });
    if (result.kind === 'branch') expect(result.value.rows).toHaveLength(1);
    const byteBounded = new PostgreSQLClient({ ...configuration, maxResponseBytes: 5 }, { resolve: async () => ({ password: new SecretString('p') }) }, { createDriver: () => driver as unknown as PgClient });
    expect(await byteBounded.query().invoke(call, { statement: 'SELECT id, amount FROM ledger' })).toMatchObject({ kind: 'branch', branch: 'truncated', value: { rows: [], truncated: true } });
    expect(driver.on).toHaveBeenCalledWith('error', expect.any(Function));
  });

  it('handles empty query response without a command tag', async () => {
    const { client } = fakeClient({ rows: [], fields: [], command: null });
    expect(await client.query().invoke(call, { statement: '-- only a comment' })).toMatchObject({ kind: 'branch', branch: 'completed', value: { columns: [], rows: [] } });
    expect(await client.execute().invoke(call, { statement: '-- only a comment' })).toMatchObject({ kind: 'branch', branch: 'completed', value: { command: '' } });
  });

  it('rolls back a RETURNING result that exceeds maxRows before CommandComplete', async () => {
    const { driver, queries } = fakeClient({ fields: [{ name: 'id', dataTypeID: 20 }], rows: [['1'], ['2'], ['3']], command: 'INSERT' });
    const bounded = new PostgreSQLClient({ ...configuration, maxRows: 2 }, { resolve: async () => ({ password: new SecretString('p') }) }, { createDriver: () => driver as unknown as PgClient });
    expect(await bounded.execute().invoke(call, { statement: 'INSERT INTO ledger SELECT generate_series(1,3) RETURNING id' })).toMatchObject({ kind: 'branch', branch: 'limitExceeded' });
    expect(queries.some(query => query.text === 'COMMIT')).toBe(false);
  });

  it('rejects duplicate or reordered JSON object keys by column', async () => {
    for (const document of ['{"a":1,"a":2}', '{"b":1,"10":2}', '{"2":1,"1":2}', '{"a":{},"nested":{"x":1,"x":2}}']) {
      const invalid = fakeClient({ fields: [{ name: 'document', dataTypeID: 114 }], rows: [[document]] });
      expect(await invalid.client.query().invoke(call, { statement: 'SELECT document' })).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'PROTOCOL', message: expect.stringContaining('document') } });
    }
  });

  it('ignores dollar signs in identifiers and accepts leading-zero placeholders', async () => {
    const { client, queries } = fakeClient();
    expect(await client.query().invoke(call, { statement: 'SELECT 1 AS col$1' })).toMatchObject({ kind: 'branch', branch: 'completed' });
    expect(await client.query().invoke(call, { statement: 'SELECT $01::int', parameters: [1] })).toMatchObject({ kind: 'branch', branch: 'completed' });
    expect(queries.at(-1)?.values).toEqual(['1']);
  });

  it.each([
    ['28P01', 'providerRejected', 'AUTHENTICATION'], ['42501', 'providerRejected', 'AUTHORIZATION'],
    ['42P01', 'providerRejected', 'NOT_FOUND'], ['23505', 'providerRejected', 'CONFLICT'],
    ['40001', 'retry', 'CONFLICT'], ['08006', 'retry', 'TRANSPORT'],
    ['57014', 'providerRejected', 'AVAILABILITY'],
  ])('classifies SQLSTATE %s as %s', async (code, branch, kind) => {
    const serverError = new pg.DatabaseError('secret provider text', 0, 'error');
    serverError.code = code;
    const { client } = fakeClient({}, { 'SELECT 1': serverError });
    const result = await client.query().invoke(call, { statement: 'SELECT 1' });
    expect(result.kind === 'retry' ? 'retry' : result.branch).toBe(branch);
    expect(result.failure?.kind).toBe(kind);
    expect(JSON.stringify(result)).not.toContain('secret provider text');
  });

  it('returns terminal credential defects and retryable refused connections', async () => {
    const missing = new PostgreSQLClient(configuration, { resolve: async () => ({ password: new SecretString('') }) });
    expect(await missing.query().invoke(call, { statement: 'SELECT 1' })).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'AUTHENTICATION' } });
    const { client } = fakeClient({}, { connect: { code: 'ECONNREFUSED' } });
    expect(await client.query().invoke(call, { statement: 'SELECT 1' })).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
  });

  it('separates malformed result values from duplicate-column defects', async () => {
    const invalid = fakeClient({ fields: [{ name: 'id', dataTypeID: 16 }], rows: [['maybe']] });
    expect(await invalid.client.query().invoke(call, { statement: 'SELECT true AS id' })).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'PROTOCOL' } });
    const duplicate = fakeClient({ fields: [{ name: 'id', dataTypeID: 20 }, { name: 'id', dataTypeID: 1700 }] });
    expect(await duplicate.client.query().invoke(call, { statement: 'SELECT 1 AS id, 2 AS id' })).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'VALIDATION' } });
    expect(await invalid.client.execute().invoke(call, { statement: 'INSERT INTO ledger VALUES (1) RETURNING true AS id' })).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'PROTOCOL' } });
  });

  it('commits a bounded idempotent write with the Dex key', async () => {
    const { client, queries } = fakeClient({ command: 'INSERT', rowCount: 1 });
    const result = await client.execute().invoke(call, { statement: 'INSERT INTO ledger VALUES ($1, $2, $3)', parameters: ['x', '1.00'], idempotencyKeyPlaceholder: 3, maxRowsAffected: 1 });
    expect(result).toMatchObject({ kind: 'branch', branch: 'completed', value: { command: 'INSERT', rowsAffected: 1 } });
    expect(queries.at(-2)?.values).toEqual(['x', '1.00', call.idempotencyKey]);
    expect(queries.at(-1)?.text).toBe('COMMIT');
  });

  it('rolls back a row-limit breach and treats a lost COMMIT as uncertain', async () => {
    const limited = fakeClient({ command: 'UPDATE', rowCount: 2, rows: [] });
    expect(await limited.client.execute().invoke(call, { statement: 'UPDATE ledger SET amount = 1', maxRowsAffected: 1 })).toMatchObject({ kind: 'branch', branch: 'limitExceeded', failure: { kind: 'CONFLICT' } });
    expect(limited.queries.some(query => query.text === 'COMMIT')).toBe(false);
    const uncertain = fakeClient({ command: 'INSERT' }, { COMMIT: new Error('socket closed') });
    expect(await uncertain.client.execute().invoke(call, { statement: 'INSERT INTO ledger VALUES (1)' })).toMatchObject({ kind: 'uncertain', failure: { kind: 'TRANSPORT' } });
    const commitError = new pg.DatabaseError('secret', 0, 'error');
    commitError.code = '23505';
    commitError.constraint = 'ledger_pkey';
    const rejected = fakeClient({ command: 'INSERT' }, { COMMIT: commitError });
    expect(await rejected.client.execute().invoke(call, { statement: 'INSERT INTO ledger VALUES (1)' })).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'CONFLICT' } });
  });

  it('retries when the driver proves COMMIT could not be sent', async () => {
    const { driver, queries } = fakeClient({ command: 'INSERT' });
    const original = driver.query;
    driver.query = vi.fn((...arguments_: Parameters<typeof original>) => {
      const response = original(...arguments_);
      if (typeof arguments_[0] === 'object') Object.assign(driver, { _queryable: false });
      return response;
    });
    const client = new PostgreSQLClient(configuration, { resolve: async () => ({ password: new SecretString('p') }) }, { createDriver: () => driver as unknown as PgClient });
    expect(await client.execute().invoke(call, { statement: 'INSERT INTO ledger VALUES (1)' })).toMatchObject({ kind: 'retry', failure: { kind: 'TRANSPORT' } });
    expect(queries.some(query => query.text === 'COMMIT')).toBe(false);
  });

  it('bounds a blackholed COMMIT and reports its outcome as uncertain', async () => {
    const { driver } = fakeClient({ command: 'INSERT', rows: [] });
    const original = driver.query;
    let rejectCommit: ((error: Error) => void) | undefined;
    driver.query = vi.fn((request: Parameters<typeof original>[0]) => {
      if (request === 'COMMIT') return new Promise((_, reject: (error: Error) => void) => { rejectCommit = reject; });
      return original(request);
    });
    Object.assign(driver, { connection: { stream: { destroy: (error: Error) => rejectCommit?.(error) } } });
    const client = new PostgreSQLClient({ ...configuration, statementTimeoutMs: 1 }, { resolve: async () => ({ password: new SecretString('p') }) }, { createDriver: () => driver as unknown as PgClient });
    vi.useFakeTimers();
    try {
      const attempt = client.execute().invoke(call, { statement: 'INSERT INTO ledger VALUES (1)' });
      await vi.runAllTimersAsync();
      expect(await attempt).toMatchObject({ kind: 'uncertain', failure: { kind: 'TRANSPORT' } });
    } finally { vi.useRealTimers(); }
  });

  it('rejects transaction control and invalid idempotency placeholders before connecting', async () => {
    const { client, driver } = fakeClient();
    expect(await client.execute().invoke(call, { statement: '/* comment */ COMMIT' })).toMatchObject({ kind: 'branch', branch: 'defect' });
    expect(await client.execute().invoke(call, { statement: 'INSERT INTO t VALUES ($1)', idempotencyKeyPlaceholder: 2 })).toMatchObject({ kind: 'branch', branch: 'defect' });
    expect(driver.connect).not.toHaveBeenCalled();
  });
});
