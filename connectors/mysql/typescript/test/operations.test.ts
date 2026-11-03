// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { Readable } from 'node:stream';
import type { ConnectionOptions } from 'mysql2/promise';
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
    expect(classifyDriverError({ fatal: true, message: "Can't add new command when connection is in closed state" }, 'commit')).toMatchObject({ disposition: 'uncertain', kind: 'TRANSPORT' });
  });
  it.each(['SELF_SIGNED_CERT_IN_CHAIN', 'UNABLE_TO_GET_ISSUER_CERT_LOCALLY', 'CERT_NOT_YET_VALID', 'ERR_TLS_CERT_ALTNAME_INVALID'])('rejects certificate failure %s', code => {
    expect(classifyDriverError({ code }, 'connect')).toMatchObject({ disposition: 'providerRejected', kind: 'AUTHENTICATION' });
    expect(classifyDriverError({ code: 'HANDSHAKE_SSL_ERROR', message: 'self-signed certificate in certificate chain', cause: { code } }, 'connect')).toMatchObject({ disposition: 'providerRejected', kind: 'AUTHENTICATION' });
    expect(classifyDriverError({ code: 'HANDSHAKE_SSL_ERROR', errno: 2026, sqlState: 'HY000', message: 'self-signed certificate in certificate chain' }, 'connect')).toMatchObject({ disposition: 'providerRejected', kind: 'AUTHENTICATION' });
  });
  it('keeps unrelated SSL handshake failures retryable', () => {
    expect(classifyDriverError({ code: 'HANDSHAKE_SSL_ERROR', message: 'socket closed' }, 'connect')).toMatchObject({ disposition: 'retry', kind: 'AVAILABILITY' });
  });
});

it.each([
  ['required', undefined],
  ['verify-ca', undefined],
  ['verify-identity', true],
] as const)('passes mysql2 TLS identity option for %s', async (sslMode, verifyIdentity) => {
  let captured: ConnectionOptions = {};
  const connection = { query: async () => [[[7, '8.4.0']], []], destroy: () => undefined };
  const client = new MySQLClient({ ...config, sslMode }, credentials, { connect: async options => { captured = options; return connection as never; } });
  await client.openSession('secret');
  expect(captured.ssl && typeof captured.ssl !== 'string' ? captured.ssl.verifyIdentity : undefined).toBe(verifyIdentity);
});

it.each(['127.0.0.1', '::1'])('rejects verify-identity for IP literal %s before connecting', async host => {
  let connections = 0;
  const client = new MySQLClient({ ...config, host, sslMode: 'verify-identity' }, credentials, { connect: async () => { connections++; throw new Error('should not connect'); } });
  expect(await client.query().invoke(call, { statement: 'SELECT 1' })).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'AUTHENTICATION', message: expect.stringContaining('IP SAN') } });
  expect(connections).toBe(0);
});

it('decodes exact MySQL type metadata and values', async () => {
  const fields = [
    { name: 'small_big', columnType: 8, flags: 0 },
    { name: 'large_big', columnType: 8, flags: 0 },
    { name: 'unsigned_big', columnType: 8, flags: 32 },
    { name: 'bits', columnType: 16, flags: 32 },
    { name: 'year_value', columnType: 13, flags: 32 },
    { name: 'single', columnType: 4, flags: 0 },
    { name: 'choice', columnType: 254, flags: 256 },
    { name: 'tags', columnType: 254, flags: 2048 },
    { name: 'document', columnType: 245, flags: 0 },
  ];
  const values = ['5', '9007199254740993', '18446744073709551615', Buffer.from([21]), '2026', Math.fround(0.1), 'open', 'a,c', '{"a":1,"b":"ok"}'];
  const connection = {
    query: async (sql: string) => sql === 'SELECT CONNECTION_ID(), VERSION()' ? [[[7, '8.4.0']], []] : [[], []],
    execute: async () => [[values], fields],
    destroy: () => undefined,
  };
  const client = new MySQLClient(config, credentials, { connect: async () => connection as never });
  const result = await client.query().invoke(call, { statement: 'SELECT * FROM exact_values' });
  expect(result).toMatchObject({ kind: 'branch', branch: 'completed', value: {
    columns: fields.map(field => ({ name: field.name, typeName: ({ small_big: 'BIGINT', large_big: 'BIGINT', unsigned_big: 'UNSIGNED BIGINT', bits: 'BIT', year_value: 'YEAR', single: 'FLOAT', choice: 'ENUM', tags: 'SET', document: 'JSON' } as Record<string, string>)[field.name] })),
    rows: [{ small_big: '5', large_big: '9007199254740993', unsigned_big: '18446744073709551615', bits: '21', year_value: 2026, single: 0.1, choice: 'open', tags: 'a,c', document: { a: 1, b: 'ok' } }],
  } });
});

it.each(['{"a":1.10}', '{"b":12345678901234567890.123456789}', '{"a":'])('rejects lossy or invalid JSON naming its column', async document => {
  const connection = {
    query: async (sql: string) => sql === 'SELECT CONNECTION_ID(), VERSION()' ? [[[7, '8.4.0']], []] : [[], []],
    execute: async () => [[[document]], [{ name: 'document', columnType: 245, flags: 0 }]],
    destroy: () => undefined,
  };
  const client = new MySQLClient(config, credentials, { connect: async () => connection as never });
  expect(await client.query().invoke(call, { statement: 'SELECT document FROM exact_values' })).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'PROTOCOL', message: expect.stringContaining('column "document"') } });
});

it.each([
  ['BIGINT', 8, 5],
  ['JSON', 245, { a: 1 }],
  ['FLOAT', 4, 0.123456789],
  ['FLOAT', 4, -0],
  ['DOUBLE', 5, -0],
] as const)('rejects driver-decoded %s values before they can lose precision', async (typeName, columnType, value) => {
  const connection = {
    query: async (sql: string) => sql === 'SELECT CONNECTION_ID(), VERSION()' ? [[[7, '8.4.0']], []] : [[], []],
    execute: async () => [[[value]], [{ name: 'typed_value', columnType, flags: 0 }]],
    destroy: () => undefined,
  };
  const client = new MySQLClient(config, credentials, { connect: async () => connection as never });
  expect(await client.query().invoke(call, { statement: 'SELECT typed_value FROM exact_values' })).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'PROTOCOL', message: expect.stringContaining(`column "typed_value" of type ${typeName}`) } });
});

it.each([
  [1048576.25, 1048576.2],
  [2097152.25, 2097152.2],
  [4144757.25, 4144757.2],
  [Math.fround(1.2621775e-29), 1.2621775e-29],
  [Math.fround(1.5474251e26), 1.5474251e26],
  [Math.fround(0.000244140625), 0.00024414063],
  [Math.fround(0.1), 0.1],
  [Math.fround(1.401298464324817e-45), 1e-45],
] as const)('decodes FLOAT %s as Go shortest decimal %s', async (single, expected) => {
  const connection = {
    query: async () => [[[7, '8.4.0']], []],
    execute: async () => [[[single]], [{ name: 'single', columnType: 4, flags: 0 }]],
    destroy: () => undefined,
  };
  const client = new MySQLClient(config, credentials, { connect: async () => connection as never });
  expect(await client.query().invoke(call, { statement: 'SELECT single FROM exact_values' })).toMatchObject({ kind: 'branch', branch: 'completed', value: { rows: [{ single: expected }] } });
});

it.each(['fatal', 'protocol', 'closing', 'destroyed'])('retries when the driver has a %s connection before COMMIT is enqueued', async connectionFailure => {
  const issued: string[] = [];
  const rawConnection = {
    _fatalError: connectionFailure === 'fatal' ? new Error('fatal') : undefined,
    _protocolError: connectionFailure === 'protocol' ? new Error('protocol') : undefined,
    _closing: connectionFailure === 'closing',
    stream: { destroyed: connectionFailure === 'destroyed' },
    authorized: true,
    get state() { return this._fatalError || this._protocolError ? 'error' : this._closing || this.stream.destroyed ? 'disconnected' : this.authorized ? 'authenticated' : 'connecting'; },
    execute: () => ({ stream: () => {
      const rows = Readable.from([], { objectMode: true });
      queueMicrotask(() => rows.emit('fields', []));
      return rows;
    } }),
  };
  const connection = {
    connection: rawConnection,
    query: async (sql: string) => {
      issued.push(sql);
      if (sql === 'SELECT CONNECTION_ID(), VERSION()') return [[[7, '8.4.0']], []];
      if (sql === 'SELECT ROW_COUNT(), LAST_INSERT_ID(), @@warning_count') return [[[1, '0', 0]], []];
      return [[], []];
    },
    destroy: () => undefined,
  };
  const client = new MySQLClient(config, credentials, { connect: async () => connection as never });
  expect(await client.execute().invoke(call, { statement: 'INSERT INTO exact_values VALUES (1)' })).toMatchObject({ kind: 'retry', failure: { kind: 'TRANSPORT' } });
  expect(issued).not.toContain('COMMIT');
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
