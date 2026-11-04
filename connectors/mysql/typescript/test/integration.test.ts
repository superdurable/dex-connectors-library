// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { createServer } from 'node:net';
import mysql from 'mysql2/promise';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { MySQLClient } from '../src/client.js';

const host = process.env.MYSQL_CONNECTOR_TEST_HOST;
if (process.env.CI && !host) throw new Error('MYSQL_CONNECTOR_TEST_HOST is required in CI');
it.skipIf(!host)('runs both operations against the CI MySQL service and rolls back a limited write', async () => {
  const database = process.env.MYSQL_CONNECTOR_TEST_DATABASE || 'connector_test';
  const user = process.env.MYSQL_CONNECTOR_TEST_USER || 'root';
  const password = process.env.MYSQL_CONNECTOR_TEST_PASSWORD || '';
  const port = Number(process.env.MYSQL_CONNECTOR_TEST_PORT || '3306');
  const sslMode = process.env.MYSQL_CONNECTOR_TEST_SSL_MODE || 'disabled';
  const administration = await mysql.createConnection({ host: host!, port, database, user, password, rowsAsArray: true, ...(sslMode === 'disabled' ? {} : { ssl: { rejectUnauthorized: false } }) });
  const table = `connector_ts_${randomUUID().replaceAll('-', '')}`;
  const provider = new MySQLClient({ host: host!, port, database, user, sslMode }, { resolve: async () => ({ password: new SecretString(password) }) });
  const call = (operationId: string): Call => ({ id: randomUUID(), idempotencyKey: randomUUID(), connection: { provider: 'mysql', name: 'live' }, operation: { connectorId: 'mysql', operationId }, context: { cancellationSignal: new AbortController().signal } }) as Call;
  try {
    await administration.query(`CREATE TABLE ${table} (id BIGINT AUTO_INCREMENT PRIMARY KEY, order_id VARCHAR(64) UNIQUE NOT NULL, amount DECIMAL(10,2) NOT NULL, idempotency_key CHAR(36) UNIQUE, big_value BIGINT, unsigned_big BIGINT UNSIGNED, bits BIT(5), single FLOAT, choice ENUM('open','closed'), tags SET('a','b'), year_value YEAR, document JSON) ENGINE=InnoDB`);
    const inserted = await provider.execute().invoke(call('execute'), { statement: `INSERT INTO ${table} (order_id, amount) VALUES (?, ?)`, parameters: ['88213', '250.00'] });
    expect(inserted).toMatchObject({ kind: 'branch', branch: 'completed', value: { rowsAffected: 1 } });
    const selected = await provider.query().invoke(call('query'), { statement: `SELECT id, order_id, amount FROM ${table} WHERE order_id = ?`, parameters: ['88213'] });
    expect(selected).toMatchObject({ kind: 'branch', branch: 'completed' });
    if (selected.kind === 'branch') expect(selected.value.rows?.[0]).toMatchObject({ id: '1', order_id: '88213', amount: '250.00' });
    await administration.query(`UPDATE ${table} SET big_value = 9007199254740993, unsigned_big = 18446744073709551615, bits = b'10101', single = 0.1, choice = 'open', tags = 'a,b', year_value = 2026, document = '{"a":1,"b":"ok"}' WHERE order_id = '88213'`);
    const typed = await provider.query().invoke(call('query'), { statement: `SELECT big_value, unsigned_big, bits, single, choice, tags, year_value, document FROM ${table} WHERE order_id = ?`, parameters: ['88213'] });
    expect(typed).toMatchObject({ kind: 'branch', branch: 'completed', value: { rows: [{ big_value: '9007199254740993', unsigned_big: '18446744073709551615', bits: '21', single: 0.1, choice: 'open', tags: 'a,b', year_value: 2026, document: { a: 1, b: 'ok' } }] } });
    if (typed.kind === 'branch') expect(Object.fromEntries((typed.value.columns ?? []).map(column => [column.name, column.typeName]))).toMatchObject({ big_value: 'BIGINT', unsigned_big: 'UNSIGNED BIGINT', bits: 'BIT', single: 'FLOAT', choice: 'ENUM', tags: 'SET', year_value: 'YEAR', document: 'JSON' });
    await administration.query(`UPDATE ${table} SET single = 1048576.25 WHERE order_id = '88213'`);
    const tiedFloat = await provider.query().invoke(call('query'), { statement: `SELECT single FROM ${table} WHERE order_id = ?`, parameters: ['88213'] });
    expect(tiedFloat).toMatchObject({ kind: 'branch', branch: 'completed', value: { rows: [{ single: 1048576.2 }] } });
    await administration.query(`UPDATE ${table} SET single = 1.2621775e-29 WHERE order_id = '88213'`);
    const boundaryFloat = await provider.query().invoke(call('query'), { statement: `SELECT single FROM ${table} WHERE order_id = ?`, parameters: ['88213'] });
    expect(boundaryFloat).toMatchObject({ kind: 'branch', branch: 'completed', value: { rows: [{ single: 1.2621775e-29 }] } });
    const [negativeZeroRows] = await administration.execute('SELECT -0e0 AS negative_zero');
    const negativeZero = await provider.query().invoke(call('query'), { statement: 'SELECT -0e0 AS negative_zero' });
    if (Object.is((negativeZeroRows as unknown[][])[0]?.[0], -0)) {
      expect(negativeZero).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'PROTOCOL', message: expect.stringContaining('column "negative_zero"') } });
    } else {
      expect(negativeZero).toMatchObject({ kind: 'branch', branch: 'completed' });
    }
    await administration.query(`UPDATE ${table} SET document = '{"a":1.10,"b":12345678901234567890.123456789}' WHERE order_id = '88213'`);
    const lossy = await provider.query().invoke(call('query'), { statement: `SELECT document FROM ${table} WHERE order_id = ?`, parameters: ['88213'] });
    expect(lossy).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'PROTOCOL', message: expect.stringContaining('column "document"') } });
    const chained = await provider.query().invoke(call('query'), { statement: `SELECT amount FROM ${table} WHERE order_id = '88213'; UPDATE ${table} SET amount = '999.00' WHERE order_id = '88213'` });
    expect(chained).toMatchObject({ kind: 'branch', branch: 'providerRejected' });
    const bounded = new MySQLClient({ host: host!, port, database, user, sslMode, maxRows: 1 }, { resolve: async () => ({ password: new SecretString(password) }) });
    const truncated = await bounded.query().invoke(call('query'), { statement: 'SELECT 1 AS n UNION ALL SELECT 2 AS n' });
    expect(truncated).toMatchObject({ kind: 'branch', branch: 'truncated', value: { rows: [{ n: '1' }] } });
    const limited = await provider.execute().invoke(call('execute'), { statement: `UPDATE ${table} SET amount = ? WHERE order_id = ?`, parameters: ['251.00', '88213'], maxRowsAffected: 0 });
    expect(limited).toMatchObject({ kind: 'branch', branch: 'limitExceeded' });
    const unchanged = await provider.query().invoke(call('query'), { statement: `SELECT amount FROM ${table} WHERE order_id = ?`, parameters: ['88213'] });
    if (unchanged.kind === 'branch') expect(unchanged.value.rows?.[0]).toMatchObject({ amount: '250.00' });
    const duplicate = await provider.execute().invoke(call('execute'), { statement: `INSERT INTO ${table} (order_id, amount) VALUES (?, ?)`, parameters: ['88213', '1.00'] });
    expect(duplicate).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'CONFLICT' } });
    const keyedCall = call('execute');
    const keyed = await provider.execute().invoke(keyedCall, { statement: `INSERT INTO ${table} (order_id, amount, idempotency_key) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE amount = amount`, parameters: ['88214', '5.00'], idempotencyKeyPlaceholder: 3 });
    expect(keyed).toMatchObject({ kind: 'branch', branch: 'completed', value: { rowsAffected: 1 } });
    const replay = await provider.execute().invoke(keyedCall, { statement: `INSERT INTO ${table} (order_id, amount, idempotency_key) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE amount = amount`, parameters: ['88214', '5.00'], idempotencyKeyPlaceholder: 3 });
    expect(replay).toMatchObject({ kind: 'branch', branch: 'completed', value: { rowsAffected: 0 } });
    const missing = await provider.query().invoke(call('query'), { statement: 'SELECT id FROM missing_mysql_connector_table' });
    expect(missing).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'NOT_FOUND' } });
    const listener = createServer();
    listener.listen(0, '127.0.0.1');
    await once(listener, 'listening');
    const address = listener.address();
    if (!address || typeof address === 'string') throw new Error('free port unavailable');
    await new Promise<void>(resolve => listener.close(() => resolve()));
    const refused = new MySQLClient({ host: '127.0.0.1', port: address.port, database, user, sslMode: 'disabled' }, { resolve: async () => ({ password: new SecretString(password) }) });
    expect(await refused.query().invoke(call('query'), { statement: 'SELECT 1' })).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
    const connectionClosedBeforeCommit = new MySQLClient({ host: host!, port, database, user, sslMode }, { resolve: async () => ({ password: new SecretString(password) }) }, { connect: async options => {
      const nativeConnection = await mysql.createConnection(options);
      return {
        connection: (nativeConnection as unknown as { connection: unknown }).connection,
        query: async (sql: string) => {
          const result = await nativeConnection.query(sql);
          if (sql === 'SELECT ROW_COUNT(), LAST_INSERT_ID(), @@warning_count') nativeConnection.destroy();
          return result;
        },
        destroy: () => nativeConnection.destroy(),
      } as never;
    } });
    expect(await connectionClosedBeforeCommit.execute().invoke(call('execute'), { statement: `UPDATE ${table} SET amount = ? WHERE order_id = ?`, parameters: ['250.00', '88213'] })).toMatchObject({ kind: 'retry', failure: { kind: 'TRANSPORT' } });
    const verifying = new MySQLClient({ host: host!, port, database, user, sslMode: 'verify-ca' }, { resolve: async () => ({ password: new SecretString(password) }) });
    if (process.env.CI && !process.env.MYSQL_TLS_CA_FILE) throw new Error('MYSQL_TLS_CA_FILE is required for MySQL TLS integration in CI');
    expect(await verifying.query().invoke(call('query'), { statement: 'SELECT 1' })).toMatchObject({ kind: 'branch', branch: 'completed' });
    const identityByIP = new MySQLClient({ host: '127.0.0.1', port, database, user, sslMode: 'verify-identity' }, { resolve: async () => ({ password: new SecretString(password) }) });
    expect(await identityByIP.query().invoke(call('query'), { statement: 'SELECT 1' })).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'AUTHENTICATION' } });
    const identityByName = new MySQLClient({ host: 'localhost', port, database, user, sslMode: 'verify-identity' }, { resolve: async () => ({ password: new SecretString(password) }) });
    expect(await identityByName.query().invoke(call('query'), { statement: 'SELECT 1' })).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'AUTHENTICATION' } });
  } finally { await administration.query(`DROP TABLE IF EXISTS ${table}`); await administration.end(); }
}, 60_000);
