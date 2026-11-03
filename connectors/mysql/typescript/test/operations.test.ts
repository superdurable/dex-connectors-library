// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { Readable } from 'node:stream';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { MySQLClient } from '../src/client.js';
import { classifyDriverError } from '../src/failures.js';

const call = { id: 'de4b2ac7-66a0-4547-b4c8-6d44a12d6029', idempotencyKey: 'de4b2ac7-66a0-4547-b4c8-6d44a12d6029', connection: { provider: 'mysql', name: 'mysql-refunds' }, operation: { connectorId: 'mysql', operationId: 'query' }, context: { cancellationSignal: new AbortController().signal } } as Call;
const config = { host: 'localhost', database: 'app', user: 'dex', sslMode: 'disabled' };
const credentials = { resolve: async () => ({ password: new SecretString('secret') }) };

describe('Go-compatible driver classification', () => {
  const server = (errno: number, sqlState: string) => ({ errno, sqlState, code: 'ER_SERVER', message: 'secret SQL must not leak' });
  it.each([
    [1045, '28000', 'connect', 'providerRejected', 'AUTHENTICATION'],
    [1142, '42000', 'statement', 'providerRejected', 'AUTHORIZATION'],
    [1146, '42S02', 'statement', 'providerRejected', 'NOT_FOUND'],
    [1062, '23000', 'statement', 'providerRejected', 'CONFLICT'],
    [1205, 'HY000', 'statement', 'retry', 'CONFLICT'],
    [1213, '40001', 'commit', 'retry', 'CONFLICT'],
    [1040, '08004', 'connect', 'retry', 'AVAILABILITY'],
    [1226, '42000', 'statement', 'providerRejected', 'QUOTA_EXHAUSTED'],
    [3024, 'HY000', 'statement', 'providerRejected', 'AVAILABILITY'],
    [1180, 'HY000', 'commit', 'uncertain', 'AVAILABILITY'],
  ] as const)('%i %s during %s becomes %s %s', (number, state, phase, disposition, kind) => {
    const classified = classifyDriverError(server(number, state), phase);
    expect(classified).toMatchObject({ disposition, kind });
    expect(JSON.stringify(classified)).not.toContain('secret SQL');
  });
  it('retries refused connections and non-COMMIT transport loss', () => {
    expect(classifyDriverError({ code: 'ECONNREFUSED' }, 'connect')).toMatchObject({ disposition: 'retry', kind: 'AVAILABILITY' });
    expect(classifyDriverError({ code: 'ECONNRESET' }, 'statement')).toMatchObject({ disposition: 'retry', kind: 'TRANSPORT' });
    expect(classifyDriverError({ code: 'ECONNRESET' }, 'commit')).toMatchObject({ disposition: 'uncertain', kind: 'TRANSPORT' });
  });
});

it('rejects invalid SQL and unavailable credentials before opening a session', async () => {
  let connections = 0;
  const client = new MySQLClient(config, credentials, { connect: async () => { connections++; throw new Error('must not connect'); } });
  const invalid = await client.query().invoke(call, { statement: 'DELETE FROM refunds' });
  expect(invalid).toMatchObject({ kind: 'branch', branch: 'defect' });
  const missing = new MySQLClient(config, { resolve: async () => { throw new Error('unavailable'); } }, { connect: async () => { connections++; throw new Error('must not connect'); } });
  expect(await missing.execute().invoke(call, { statement: 'INSERT INTO refunds VALUES (1)' })).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'AUTHENTICATION' } });
  expect(connections).toBe(0);
});

it('routes bounded query results, write limit rollback, and commit uncertainty', async () => {
  const issued: string[] = [];
  let shouldFailCommit = false;
  const connection = {
    query: async (sql: string) => {
      issued.push(sql);
      if (sql === 'SELECT CONNECTION_ID(), VERSION()') return [[[12, '8.4.0']], []];
      if (sql === 'SELECT ROW_COUNT(), LAST_INSERT_ID(), @@warning_count') return [[[2, '7', 0]], []];
      if (sql === 'COMMIT' && shouldFailCommit) throw { code: 'ECONNRESET' };
      return [[], []];
    },
    execute: async (sql: string, parameters: unknown[]) => {
      issued.push(sql);
      expect(parameters).toBeDefined();
      return [sql.startsWith('SELECT') ? [[1], [2]] : [], sql.startsWith('SELECT') ? [{ name: 'id', columnType: 3, flags: 0 }] : []];
    },
    destroy: () => { issued.push('destroy'); },
  };
  const client = new MySQLClient({ ...config, maxRows: 1 }, credentials, { connect: async () => connection as never });
  expect(await client.query().invoke(call, { statement: 'SELECT id FROM refunds' })).toMatchObject({ kind: 'branch', branch: 'truncated', value: { rows: [{ id: 1 }] } });
  expect(issued).toContain('START TRANSACTION READ ONLY');
  const limited = await client.execute().invoke(call, { statement: 'INSERT INTO refunds VALUES (?)', parameters: ['one'], maxRowsAffected: 1 });
  expect(limited).toMatchObject({ kind: 'branch', branch: 'limitExceeded' });
  expect(issued).not.toContain('COMMIT');
  shouldFailCommit = true;
  expect(await client.execute().invoke(call, { statement: 'INSERT INTO refunds VALUES (?)', parameters: ['one'] })).toMatchObject({ kind: 'uncertain' });
});

it('bounds native prepared-statement rows while streaming', async () => {
  let bufferedExecuteCalls = 0;
  const connection = {
    query: async (sql: string) => sql === 'SELECT CONNECTION_ID(), VERSION()' ? [[[7, '8.4.0']], []] : [[], []],
    execute: async () => { bufferedExecuteCalls++; throw new Error('buffered execute must not run'); },
    connection: {
      execute: () => ({ stream: () => {
        const rows = Readable.from([[1], [2], [3]], { objectMode: true, highWaterMark: 1 });
        queueMicrotask(() => rows.emit('fields', [{ name: 'id', columnType: 3, flags: 0 }]));
        return rows;
      } }),
    },
    destroy: () => undefined,
  };
  const client = new MySQLClient({ ...config, maxRows: 1 }, credentials, { connect: async () => connection as never });
  expect(await client.query().invoke(call, { statement: 'SELECT id FROM refund_ledger' })).toMatchObject({ kind: 'branch', branch: 'truncated', value: { rows: [{ id: 1 }] } });
  expect(bufferedExecuteCalls).toBe(0);
});
