// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Client as PgClient } from 'pg';
import Cursor from 'pg-cursor';
import { describe, expect, it, vi } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { PostgreSQLClient } from '../src/client.js';

const call = { id: '00000000-0000-4000-8000-000000000001', idempotencyKey: '00000000-0000-4000-8000-000000000001' } as Call;
const configuration = { host: '127.0.0.1', database: 'app', user: 'dex_app', sslMode: 'disable' };
const columns = [{ name: 'id', dataTypeID: 20 }, { name: 'amount', dataTypeID: 1700 }];

function fakeClient(statementResult: { rows?: unknown[][]; fields?: typeof columns; rowCount?: number; command?: string } = {}, errors: Record<string, unknown> = {}) {
  const queries: Array<{ text: string; values?: unknown[] }> = [];
  const driver = {
    processID: 1001,
    connect: vi.fn(async () => { if (errors.connect) throw errors.connect; }),
    end: vi.fn(async () => undefined),
    query: vi.fn((request: string | { text: string; values?: unknown[] }) => {
      const text = typeof request === 'string' ? request : request.text;
      queries.push({ text, ...(typeof request === 'string' ? {} : { values: request.values }) });
      if (request instanceof Cursor) {
        let offset = 0;
        const sourceRows = statementResult.rows ?? [['9007199254740993', '1200.00']];
        Object.assign(request, {
          read: (count: number, callback: (error: Error | undefined, rows: unknown[][], result: unknown) => void) => {
            if (errors[text]) { callback(errors[text] as Error, [], undefined); return; }
            const rows = sourceRows.slice(offset, offset + count);
            offset += rows.length;
            callback(undefined, rows, { command: statementResult.command ?? 'SELECT', fields: statementResult.fields ?? columns, rowCount: statementResult.rowCount ?? 1 });
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

  it('routes row and byte bounds to truncated without committing', async () => {
    const { driver } = fakeClient({ rows: [['1', '1.00'], ['2', '2.00']], rowCount: 2 });
    const bounded = new PostgreSQLClient({ ...configuration, maxRows: 1 }, { resolve: async () => ({ password: new SecretString('p') }) }, { createDriver: () => driver as unknown as PgClient });
    const result = await bounded.query().invoke(call, { statement: 'SELECT id, amount FROM ledger' });
    expect(result).toMatchObject({ kind: 'branch', branch: 'truncated', value: { truncated: true }, failure: { kind: 'RESPONSE_TOO_LARGE' } });
  });

  it.each([
    ['28P01', 'providerRejected', 'AUTHENTICATION'], ['42501', 'providerRejected', 'AUTHORIZATION'],
    ['42P01', 'providerRejected', 'NOT_FOUND'], ['23505', 'providerRejected', 'CONFLICT'],
    ['40001', 'retry', 'CONFLICT'], ['08006', 'retry', 'TRANSPORT'],
    ['57014', 'providerRejected', 'AVAILABILITY'],
  ])('classifies SQLSTATE %s as %s', async (code, branch, kind) => {
    const { client } = fakeClient({}, { 'SELECT 1': { code, message: 'secret provider text' } });
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
    const rejected = fakeClient({ command: 'INSERT' }, { COMMIT: { code: '23505', constraint: 'ledger_pkey', message: 'secret' } });
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

  it('rejects transaction control and invalid idempotency placeholders before connecting', async () => {
    const { client, driver } = fakeClient();
    expect(await client.execute().invoke(call, { statement: '/* comment */ COMMIT' })).toMatchObject({ kind: 'branch', branch: 'defect' });
    expect(await client.execute().invoke(call, { statement: 'INSERT INTO t VALUES ($1)', idempotencyKeyPlaceholder: 2 })).toMatchObject({ kind: 'branch', branch: 'defect' });
    expect(driver.connect).not.toHaveBeenCalled();
  });
});
