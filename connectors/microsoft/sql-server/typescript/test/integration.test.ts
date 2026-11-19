// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import sql from 'mssql';
import { expect, it } from 'vitest';
import { SecretString } from '@superdurable/connectors-sdk';
import { SQLServerClient } from '../src/client.js';

it('queries server types, replays an idempotent write, and rolls back a row-limit violation', async () => {
  const host = process.env.SQLSERVER_CONNECTOR_TEST_HOST;
  const password = process.env.SQLSERVER_CONNECTOR_TEST_PASSWORD;
  if (!host || !password) throw new Error('SQLSERVER_CONNECTOR_TEST_HOST and PASSWORD are required');
  const tlsCAFile = process.env.SQLSERVER_TLS_CA_FILE;
  const tlsHost = process.env.SQLSERVER_TLS_HOST;
  if (tlsCAFile && !tlsHost) throw new Error('SQLSERVER_TLS_HOST is required when SQLSERVER_TLS_CA_FILE is set');
  if (tlsCAFile && process.env.SQLSERVER_SSL_CA !== tlsCAFile) throw new Error('SQLSERVER_SSL_CA must name the mandatory-TLS test bundle');
  const config = { host: tlsHost || host, port: Number(process.env.SQLSERVER_CONNECTOR_TEST_PORT), database: process.env.SQLSERVER_CONNECTOR_TEST_DATABASE || 'master', user: process.env.SQLSERVER_CONNECTOR_TEST_USER || 'sa', encrypt: tlsCAFile ? 'mandatory' : process.env.SQLSERVER_CONNECTOR_TEST_ENCRYPT || 'mandatory' };
  const provider = new SQLServerClient(config, { resolve: async () => ({ password: new SecretString(password) }) });
  const admin = await new sql.ConnectionPool({ server: config.host, port: config.port, database: config.database, user: config.user, password, options: { encrypt: config.encrypt !== 'disable', trustServerCertificate: config.encrypt === 'trust-server-certificate', ...(tlsCAFile ? { cryptoCredentialsDetails: { ca: await readFile(tlsCAFile) } } : {}) } }).connect();
  const table = `dex_connector_ts_${randomUUID().replaceAll('-', '')}`;
  const call = { id: randomUUID(), idempotencyKey: randomUUID(), context: { cancellationSignal: new AbortController().signal } } as Parameters<ReturnType<SQLServerClient['query']>['invoke']>[0];
  try {
    if (tlsCAFile) {
      const wrongHost = new SQLServerClient({ ...config, host: '127.0.0.2' }, { resolve: async () => ({ password: new SecretString(password) }) });
      const rejectedHost = await wrongHost.query().invoke(call, { statement: 'SELECT 1 AS answer', parameters: [] });
      expect(rejectedHost).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'AUTHENTICATION' } });
    }
    await admin.request().query(`CREATE TABLE dbo.${table} (id int IDENTITY PRIMARY KEY, idempotency_key uniqueidentifier NOT NULL UNIQUE, note nvarchar(20) NOT NULL, amount money NOT NULL DEFAULT 1.2300)`);
    const query = await provider.query().invoke(call, { statement: "SELECT CAST(@p1 AS bigint) AS big, CAST(@p2 AS decimal(12,2)) AS exact, CAST(N'héllo' AS nvarchar(10)) AS label", parameters: ['9007199254740993', '1200.5'] });
    expect(query.kind).toBe('branch');
    if (query.kind === 'branch') expect(query.value.rows?.[0]).toMatchObject({ big: '9007199254740993', exact: '1200.50', label: 'héllo' });
    const ordered = await provider.query().invoke(call, { statement: 'SELECT 1 AS [2], 2 AS [1]', parameters: [] });
    expect(ordered.kind).toBe('branch');
    if (ordered.kind === 'branch') expect(ordered.value.columns?.map(column => column?.name)).toEqual(['2', '1']);
    const jsonChunks = await provider.query().invoke(call, { statement: 'SELECT 1 AS answer FOR JSON PATH', parameters: [] });
    expect(jsonChunks).toMatchObject({ kind: 'branch', branch: 'completed' });
    if (jsonChunks.kind === 'branch') {
      const columnName = jsonChunks.value.columns?.[0]?.name;
      expect(columnName).toBe('JSON_F52E2B61-18A1-11d1-B105-00805F49916B');
      expect(JSON.parse(jsonChunks.value.rows?.map(row => row?.[columnName!]).join('') ?? '')).toEqual([{ answer: 1 }]);
    }
    const multipleJsonChunks = await provider.query().invoke(call, { statement: "SELECT REPLICATE(CAST(N'X' AS nvarchar(max)), 5000) AS answer FOR JSON PATH", parameters: [] });
    expect(multipleJsonChunks).toMatchObject({ kind: 'branch', branch: 'completed' });
    if (multipleJsonChunks.kind === 'branch') {
      const columnName = multipleJsonChunks.value.columns?.[0]?.name;
      expect(multipleJsonChunks.value.rows?.length).toBeGreaterThan(1);
      expect(JSON.parse(multipleJsonChunks.value.rows?.map(row => row?.[columnName!]).join('') ?? '')[0].answer).toHaveLength(5000);
    }
    const xmlChunks = await provider.query().invoke(call, { statement: "SELECT 1 AS answer FOR XML PATH('row')", parameters: [] });
    expect(xmlChunks).toMatchObject({ kind: 'branch', branch: 'completed' });
    if (xmlChunks.kind === 'branch') {
      const columnName = xmlChunks.value.columns?.[0]?.name;
      expect(columnName).toBe('XML_F52E2B61-18A1-11d1-B105-00805F49916B');
      expect(xmlChunks.value.rows?.map(row => row?.[columnName!]).join('')).toContain('<answer>1</answer>');
    }
    const duplicateColumns = await provider.query().invoke(call, { statement: 'SELECT 1 AS repeated, 2 AS repeated', parameters: [] });
    expect(duplicateColumns.kind).toBe('branch');
    if (duplicateColumns.kind === 'branch') { expect(duplicateColumns.branch).toBe('defect'); expect(duplicateColumns.failure?.kind).toBe('VALIDATION'); }
    const unnamedColumn = await provider.query().invoke(call, { statement: 'SELECT 1', parameters: [] });
    expect(unnamedColumn.kind).toBe('branch');
    if (unnamedColumn.kind === 'branch') { expect(unnamedColumn.branch).toBe('defect'); expect(unnamedColumn.failure?.kind).toBe('VALIDATION'); }
    const settings = await provider.query().invoke(call, { statement: 'SELECT @@LANGUAGE AS language, @@LOCK_TIMEOUT AS lock_timeout', parameters: [] });
    expect(settings.kind).toBe('branch');
    if (settings.kind === 'branch') {
      expect(settings.branch).toBe('completed');
      expect(settings.value.rows?.[0]).toMatchObject({ language: 'us_english', lock_timeout: 5000 });
      expect(settings.receipt.metadata?.processId).toMatch(/^\d+$/);
      expect(settings.receipt.metadata?.serverVersion).toMatch(/^\d/);
    }
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
    expect(limited.kind).toBe('branch');
    if (limited.kind === 'branch') expect(limited.branch).toBe('limitExceeded');
    const rollbackQuery = await provider.query().invoke(call, { statement: `WITH target AS (SELECT note FROM dbo.${table}) UPDATE target SET note = @p1 OUTPUT INSERTED.note`, parameters: ['query-changed'] });
    expect(rollbackQuery.kind).toBe('branch');
    if (rollbackQuery.kind === 'branch') expect(rollbackQuery.branch).toBe('completed');
    const unsupportedWrite = await provider.execute().invoke(call, { statement: `UPDATE dbo.${table} SET note = @p1 OUTPUT INSERTED.amount`, parameters: ['changed'], returnsOutputRows: true });
    expect(unsupportedWrite.kind).toBe('branch');
    if (unsupportedWrite.kind === 'branch') {
      expect(unsupportedWrite.branch).toBe('invalidResponse');
      expect(unsupportedWrite.failure?.message).toContain('column "amount" of type MONEY');
    }
    const persisted = await admin.request().query<{ note: string }>(`SELECT note FROM dbo.${table}`);
    expect(persisted.recordset[0]?.note).toBe('first');
    const duplicate = await provider.execute().invoke(call, { statement: `INSERT INTO dbo.${table} (idempotency_key, note) VALUES (CAST(@p1 AS uniqueidentifier), @p2)`, parameters: [call.idempotencyKey, 'duplicate'] });
    expect(duplicate.kind).toBe('branch');
    if (duplicate.kind === 'branch') {
      expect(duplicate.branch).toBe('providerRejected');
      expect(duplicate.failure?.kind).toBe('CONFLICT');
      expect(duplicate.receipt.metadata?.errorNumber).toBe('2627');
    }
    const bounded = new SQLServerClient({ ...config, maxRows: 1 }, { resolve: async () => ({ password: new SecretString(password) }) });
    const truncated = await bounded.query().invoke(call, { statement: 'SELECT v AS answer FROM (VALUES (1), (2)) source(v) ORDER BY v', parameters: [] });
    expect(truncated.kind).toBe('branch');
    if (truncated.kind === 'branch') {
      expect(truncated.branch).toBe('truncated');
      expect(truncated.value.rows).toEqual([{ answer: 1 }]);
    }
    const byteBounded = new SQLServerClient({ ...config, maxResponseBytes: 2 }, { resolve: async () => ({ password: new SecretString(password) }) });
    const byteTruncated = await byteBounded.query().invoke(call, { statement: "SELECT N'wide result' AS answer", parameters: [] });
    expect(byteTruncated.kind).toBe('branch');
    if (byteTruncated.kind === 'branch') { expect(byteTruncated.branch).toBe('truncated'); expect(byteTruncated.value.rows).toEqual([]); }
    const largeMaxValue = await byteBounded.query().invoke(call, { statement: "SELECT REPLICATE(CAST(N'X' AS nvarchar(max)), 2000000) AS answer", parameters: [] });
    expect(largeMaxValue).toMatchObject({ kind: 'branch', branch: 'truncated' });
    const largeJson = await byteBounded.query().invoke(call, { statement: "SELECT REPLICATE(CAST(N'X' AS nvarchar(max)), 2000000) AS answer FOR JSON PATH", parameters: [] });
    expect(largeJson).toMatchObject({ kind: 'branch', branch: 'truncated' });
    const outputBounded = await byteBounded.execute().invoke(call, { statement: `UPDATE dbo.${table} SET note = @p1 OUTPUT INSERTED.note`, parameters: ['too-wide'], returnsOutputRows: true });
    expect(outputBounded.kind).toBe('branch');
    if (outputBounded.kind === 'branch') expect(outputBounded.branch).toBe('limitExceeded');
    const afterOutputLimit = await admin.request().query<{ note: string }>(`SELECT note FROM dbo.${table}`);
    expect(afterOutputLimit.recordset[0]?.note).toBe('first');
  } finally {
    try { await admin.request().query(`DROP TABLE IF EXISTS dbo.${table}`); } finally { await admin.close(); }
  }
}, 60_000);
