// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import sql from 'mssql';
import { expect, it } from 'vitest';
import { SecretString } from '@superdurable/connectors-sdk';
import { SQLServerClient } from '../src/client.js';

it('queries server types, replays an idempotent write, and rolls back a row-limit violation', async () => {
  const host = process.env.SQLSERVER_CONNECTOR_TEST_HOST;
  const password = process.env.SQLSERVER_CONNECTOR_TEST_PASSWORD;
  if (!host || !password) throw new Error('SQLSERVER_CONNECTOR_TEST_HOST and PASSWORD are required');
  const config = { host, port: Number(process.env.SQLSERVER_CONNECTOR_TEST_PORT), database: process.env.SQLSERVER_CONNECTOR_TEST_DATABASE || 'master', user: process.env.SQLSERVER_CONNECTOR_TEST_USER || 'sa', encrypt: process.env.SQLSERVER_CONNECTOR_TEST_ENCRYPT || 'mandatory' };
  const provider = new SQLServerClient(config, { resolve: async () => ({ password: new SecretString(password) }) });
  const admin = await new sql.ConnectionPool({ server: host, port: config.port, database: config.database, user: config.user, password, options: { encrypt: config.encrypt !== 'disable', trustServerCertificate: config.encrypt === 'trust-server-certificate' } }).connect();
  const table = `dex_connector_ts_${randomUUID().replaceAll('-', '')}`;
  const call = { id: randomUUID(), idempotencyKey: randomUUID(), context: { cancellationSignal: new AbortController().signal } } as Parameters<ReturnType<SQLServerClient['query']>['invoke']>[0];
  try {
    await admin.request().query(`CREATE TABLE dbo.${table} (id int IDENTITY PRIMARY KEY, idempotency_key uniqueidentifier NOT NULL UNIQUE, note nvarchar(20) NOT NULL, amount money NOT NULL DEFAULT 1.2300)`);
    const query = await provider.query().invoke(call, { statement: "SELECT CAST(@p1 AS bigint) AS big, CAST(@p2 AS decimal(12,2)) AS exact, CAST(N'héllo' AS nvarchar(10)) AS label", parameters: ['9007199254740993', '1200.5'] });
    expect(query.kind).toBe('branch');
    if (query.kind === 'branch') expect(query.value.rows?.[0]).toMatchObject({ big: '9007199254740993', exact: '1200.50', label: 'héllo' });
    const types = await provider.query().invoke(call, {
      statement: "SELECT CAST(0.1 AS real) AS real_value, CAST('2024-01-02' AS date) AS date_value, CAST('12:34:56.1234567' AS time(7)) AS time_value, CAST('2024-01-02T12:34:56.1234567' AS datetime2(7)) AS datetime2_value, CAST('2024-01-02T12:34:56.123' AS datetime) AS datetime_value, CAST('2024-01-02T12:34:00' AS smalldatetime) AS smalldatetime_value, CAST('AABBCCDD-0123-ABCD-9876-001122334455' AS uniqueidentifier) AS guid_value",
      parameters: [],
    });
    expect(types.kind).toBe('branch');
    if (types.kind === 'branch') expect(types.value.rows?.[0]).toMatchObject({
      real_value: 0.1, date_value: '2024-01-02', time_value: '12:34:56.1234567',
      datetime2_value: '2024-01-02T12:34:56.1234567', datetime_value: '2024-01-02T12:34:56.123',
      smalldatetime_value: '2024-01-02T12:34:00', guid_value: 'aabbccdd-0123-abcd-9876-001122334455',
    });
    for (const [expression, typeName] of [
      ["CAST('1234567890123456.12' AS decimal(18,2))", 'DECIMAL'],
      ["CAST(1.2345 AS money)", 'MONEY'],
      ["CAST(1.2345 AS smallmoney)", 'SMALLMONEY'],
      ["CAST('2024-01-02T12:34:56.1234567+05:30' AS datetimeoffset(7))", 'DATETIMEOFFSET'],
    ]) {
      const unsupported = await provider.query().invoke(call, { statement: `SELECT ${expression} AS answer`, parameters: [] });
      expect(unsupported.kind).toBe('branch');
      if (unsupported.kind === 'branch') {
        expect(unsupported.branch).toBe('invalidResponse');
        expect(unsupported.failure?.message).toContain(`column "answer" of type ${typeName}`);
        expect(unsupported.value.rows).toEqual([]);
      }
    }
    const input = { statement: `INSERT INTO dbo.${table} (idempotency_key, note) OUTPUT INSERTED.id SELECT CAST(@p2 AS uniqueidentifier), @p1 WHERE NOT EXISTS (SELECT 1 FROM dbo.${table} WITH (UPDLOCK, HOLDLOCK) WHERE idempotency_key = CAST(@p2 AS uniqueidentifier))`, parameters: ['first'], idempotencyKeyPlaceholder: 2, returnsOutputRows: true };
    const first = await provider.execute().invoke(call, input);
    const replay = await provider.execute().invoke(call, input);
    expect(first.kind).toBe('branch');
    expect(replay.kind).toBe('branch');
    if (first.kind === 'branch' && replay.kind === 'branch') {
      expect(first.value.rowsAffected).toBe(1);
      expect(replay.value.rowsAffected).toBe(0);
    }
    const limited = await provider.execute().invoke(call, { statement: `UPDATE dbo.${table} SET note = @p1`, parameters: ['changed'], maxRowsAffected: 0 });
    if (limited.kind === 'branch') expect(limited.branch).toBe('limitExceeded');
    const unsupportedWrite = await provider.execute().invoke(call, { statement: `UPDATE dbo.${table} SET note = @p1 OUTPUT INSERTED.amount`, parameters: ['changed'], returnsOutputRows: true });
    expect(unsupportedWrite.kind).toBe('branch');
    if (unsupportedWrite.kind === 'branch') {
      expect(unsupportedWrite.branch).toBe('invalidResponse');
      expect(unsupportedWrite.failure?.message).toContain('column "amount" of type MONEY');
    }
    const persisted = await admin.request().query<{ note: string }>(`SELECT note FROM dbo.${table}`);
    expect(persisted.recordset[0]?.note).toBe('first');
  } finally {
    try { await admin.request().query(`DROP TABLE IF EXISTS dbo.${table}`); } finally { await admin.close(); }
  }
}, 60_000);
