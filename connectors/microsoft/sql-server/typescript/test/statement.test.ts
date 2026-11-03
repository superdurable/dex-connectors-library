// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import sql from 'mssql';
import { bindParameters, validateStatement } from '../src/operations.js';

it.each([
  'UPDATE dbo.t SET x = @p1 DROP TABLE dbo.y',
  'UPDATE dbo.t SET x = @p1 /* outer /* inner */ */ BACKUP DATABASE x TO DISK = N\'x\'',
  'UPDATE dbo.t SET x = @p1 WAITFOR DELAY \'00:00:01\'',
  'UPDATE dbo.t SET x = @p1; SELECT 1',
  'UPDATE dbo.t SET x = @p1 EXEC sp_who',
  'UPDATE dbo.t SET x = @p1 OPENQUERY(link, \'SELECT 1\')',
  'UPDATE dbo.t SET x = @p1 PRINT \'x\'',
])('rejects a second or forbidden statement: %s', statement => {
  expect(() => validateStatement(statement, [1], true)).toThrow();
});

it('honors nested comments, delimited identifiers, system variables, and USE HINT', () => {
  expect(() => validateStatement("(SELECT @@ROWCOUNT AS [DROP], @p1 AS \"CREATE\" /* outer /* EXEC */ */ OPTION (USE HINT ('DISABLE_OPTIMIZER_ROWGOAL'))) ;", [1], false)).not.toThrow();
  expect(() => validateStatement('SELECT @p1 /* unterminated', [1], false)).toThrow('unterminated');
  expect(() => validateStatement('SELECT @p1 AS [unterminated', [1], false)).toThrow('unterminated');
});

it.each(['@p01', '@P1', '@p0', '@other', '@p2'])('rejects invalid or unbound placeholder %s', placeholder => {
  expect(() => validateStatement(`SELECT ${placeholder}`, [1], false)).toThrow();
});

it('enforces Go statement and parameter bounds', () => {
  expect(() => validateStatement(`SELECT @p1 /*${'x'.repeat(70_000)}*/`, [1], false)).not.toThrow();
  expect(() => validateStatement(`SELECT @p1 /*${'x'.repeat(1048576)}*/`, [1], false)).toThrow();
  expect(() => validateStatement('SELECT @p1', [Number.NaN], false)).toThrow();
  expect(() => validateStatement('SELECT @p1', [Number.POSITIVE_INFINITY], false)).toThrow();
  expect(() => validateStatement('SELECT @p1', [Number.MAX_SAFE_INTEGER + 1], false)).toThrow();
  expect(() => validateStatement('SELECT @p1', [Buffer.alloc(16 * 1048576 + 1)], false)).toThrow();
  expect(() => validateStatement('SELECT 1', new Array(2099).fill(null), false)).toThrow('too many');
});

it('binds dates as UTC datetime2 and numbers without mssql inference', () => {
  const request = new sql.Request();
  bindParameters(request, [new Date('2024-01-02T03:04:05.123Z'), 42, 0.5, true, 'text', Buffer.from([1])]);
  expect((request.parameters.p1?.type as { name: string }).name).toBe('DateTime2');
  expect((request.parameters.p2?.type as { name: string }).name).toBe('BigInt');
  expect((request.parameters.p3?.type as { name: string }).name).toBe('Float');
  expect((request.parameters.p4?.type as { name: string }).name).toBe('Bit');
  expect((request.parameters.p5?.type as { name: string }).name).toBe('NVarChar');
  expect((request.parameters.p6?.type as { name: string }).name).toBe('VarBinary');
});
