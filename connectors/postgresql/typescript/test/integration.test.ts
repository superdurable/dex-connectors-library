// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { PostgreSQLClient } from '../src/client.js';

it('runs the Go live-test transaction, exact-type, read-only, rollback, and idempotency scenarios', async () => {
  const host = process.env.POSTGRESQL_CONNECTOR_TEST_HOST;
  if (!host) throw new Error('POSTGRESQL_CONNECTOR_TEST_HOST is required for the native-driver CI job');
  const client = new PostgreSQLClient({
    host,
    port: Number(process.env.POSTGRESQL_CONNECTOR_TEST_PORT ?? '5432'),
    database: process.env.POSTGRESQL_CONNECTOR_TEST_DATABASE ?? '',
    user: process.env.POSTGRESQL_CONNECTOR_TEST_USER ?? '',
    sslMode: process.env.POSTGRESQL_CONNECTOR_TEST_SSL_MODE ?? 'require',
    maxRows: 2,
  }, { resolve: async () => ({ password: new SecretString(process.env.POSTGRESQL_CONNECTOR_TEST_PASSWORD ?? '') }) });
  const call = { id: randomUUID(), idempotencyKey: randomUUID() } as Call;
  const table = `connector_live_${randomUUID().replaceAll('-', '')}`;
  const execute = (statement: string, parameters?: Array<unknown | null>, maxRowsAffected?: number) => client.execute().invoke(call, { statement, ...(parameters ? { parameters } : {}), ...(maxRowsAffected === undefined ? {} : { maxRowsAffected }) });
  const query = (statement: string, parameters?: Array<unknown | null>) => client.query().invoke(call, { statement, ...(parameters ? { parameters } : {}) });
  expect(await execute(`CREATE TABLE ${table} (id uuid PRIMARY KEY, amount numeric(12,2), label text)`)).toMatchObject({ kind: 'branch', branch: 'completed' });
  try {
    expect(await query(`SELECT '9007199254740993'::int8 AS big, '1200.00'::numeric AS exact, true AS flag, '\\x0001ff'::bytea AS payload, '{"a":1}'::jsonb AS document`)).toMatchObject({ kind: 'branch', branch: 'completed', value: { rows: [{ big: '9007199254740993', exact: '1200.00', flag: true, payload: 'AAH/', document: { a: 1 } }] } });
    expect(await query(`INSERT INTO ${table} VALUES ($1, $2, $3)`, [randomUUID(), '1.00', 'blocked'])).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'AUTHORIZATION' } });
    const id = randomUUID();
    expect(await execute(`INSERT INTO ${table} VALUES ($1, $2, $3) ON CONFLICT (id) DO NOTHING`, [id, '250.00', 'first'], 1)).toMatchObject({ kind: 'branch', branch: 'completed', value: { rowsAffected: 1 } });
    expect(await execute(`INSERT INTO ${table} VALUES ($1, $2, $3) ON CONFLICT (id) DO NOTHING`, [id, '250.00', 'first'], 1)).toMatchObject({ kind: 'branch', branch: 'completed', value: { rowsAffected: 0 } });
    expect(await execute(`INSERT INTO ${table} VALUES ($1, $2, $3)`, [id, '1.00', 'duplicate'])).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'CONFLICT' } });
    expect(await execute(`UPDATE ${table} SET label = 'changed'`, undefined, 0)).toMatchObject({ kind: 'branch', branch: 'limitExceeded' });
    expect(await query(`SELECT label FROM ${table} WHERE id = $1`, [id])).toMatchObject({ kind: 'branch', branch: 'completed', value: { rows: [{ label: 'first' }] } });
    expect(await query('SELECT FROM missing_connector_table')).toMatchObject({ kind: 'branch', branch: 'providerRejected' });
  } finally {
    expect(await execute(`DROP TABLE IF EXISTS ${table}`)).toMatchObject({ kind: 'branch', branch: 'completed' });
  }
}, 90_000);
