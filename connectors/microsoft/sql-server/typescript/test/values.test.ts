// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import type sql from 'mssql';
import { decodeRows, InvalidResultError, ResultShapeError } from '../src/operations.js';

function result(typeName: string, value: unknown, precision?: number, scale?: number): sql.IResult<Record<string, unknown>> {
  const rows = [{ answer: value }] as unknown as sql.IRecordSet<Record<string, unknown>>;
  rows.columns = { answer: { index: 0, name: 'answer', length: 8, type: { name: typeName } as unknown as sql.ISqlType, precision, scale, nullable: false, caseSensitive: false, identity: false, readOnly: true } };
  return { recordset: rows, recordsets: [rows], rowsAffected: [1], output: {} };
}

function temporal(iso: string, nanosecondsDelta = 0): Date {
  const date = new Date(iso) as Date & { nanosecondsDelta: number };
  date.nanosecondsDelta = nanosecondsDelta;
  return date;
}

it.each([
  ['Decimal', 1200.5, 12, 2, '1200.50'],
  ['Numeric', -0.125, 10, 3, '-0.125'],
  ['Decimal', -123456789012.34, 15, 2, '-123456789012.34'],
  ['BigInt', '9007199254740993', undefined, undefined, '9007199254740993'],
  ['Bit', true, undefined, undefined, true],
  ['TinyInt', 255, undefined, undefined, 255],
  ['SmallInt', -100, undefined, undefined, -100],
  ['Int', 12345, undefined, undefined, 12345],
  ['Float', 0.125, undefined, undefined, 0.125],
  ['NVarChar', 'héllo', undefined, undefined, 'héllo'],
  ['VarBinary', Buffer.from([0, 1, 255]), undefined, undefined, 'AAH/'],
  ['Int', null, undefined, undefined, null],
  ['UniqueIdentifier', 'AABBCCDD-0123-ABCD-9876-001122334455', undefined, undefined, 'aabbccdd-0123-abcd-9876-001122334455'],
  ['Real', Math.fround(0.1), undefined, undefined, 0.1],
  ['Date', temporal('2024-01-02T00:00:00Z'), undefined, undefined, '2024-01-02'],
  ['Time', temporal('1970-01-01T12:34:56.123Z', 0.0004567), undefined, undefined, '12:34:56.1234567'],
  ['DateTime2', temporal('2024-01-02T12:34:56.123Z', 0.0004567), undefined, undefined, '2024-01-02T12:34:56.1234567'],
  ['DateTime', temporal('2024-01-02T12:34:56.123Z'), undefined, undefined, '2024-01-02T12:34:56.123'],
  ['SmallDateTime', temporal('2024-01-02T12:34:00Z'), undefined, undefined, '2024-01-02T12:34:00'],
])('matches Go %s result values', (typeName, value, precision, scale, expected) => {
  expect(decodeRows(result(typeName, value, precision, scale), 10, 1000).rows[0]?.answer).toEqual(expected);
});

it('detects duplicate and empty names in ordered array-row metadata', () => {
  const reply = result('Int', 1);
  const column = Object.values(reply.recordset.columns)[0]!;
  (reply.recordset as unknown as { columns: unknown }).columns = [{ ...column, name: 'x' }, { ...column, name: 'x' }];
  expect(() => decodeRows(reply, 10, 1000)).toThrow(ResultShapeError);
  (reply.recordset as unknown as { columns: unknown }).columns = [{ ...column, name: '' }];
  expect(() => decodeRows(reply, 10, 1000)).toThrow('no name');
});

it('rejects multiple result sets and bounds rows and encoded bytes', () => {
  const reply = result('NVarChar', 'abcd');
  expect(() => decodeRows({ ...reply, recordsets: [reply.recordset, reply.recordset] }, 10, 1000)).toThrow('more than one result set');
  reply.recordset.push({ answer: 'second' });
  expect(decodeRows(reply, 1, 1000)).toMatchObject({ truncated: true, rows: [{ answer: 'abcd' }] });
  expect(decodeRows(reply, 10, 2)).toMatchObject({ truncated: true, rows: [] });
});

it('rejects sql_variant and CLR types before any row is read', () => {
  for (const typeName of ['Variant', 'Geography', 'Geometry', 'HierarchyId']) {
    const reply = result(typeName, Buffer.from([1]));
    Object.defineProperty(reply.recordset[0], 'answer', { get: () => { throw new Error('row was read'); } });
    expect(() => decodeRows(reply, 10, 1000)).toThrow(InvalidResultError);
  }
});

it.each([
  ['Decimal', 16, 2], ['Numeric', 16, 2], ['Money', undefined, undefined],
  ['SmallMoney', undefined, undefined], ['DateTimeOffset', undefined, undefined],
])('rejects lossy %s from metadata before reading rows', (typeName, precision, scale) => {
  const reply = result(typeName, 123, precision, scale);
  Object.defineProperty(reply.recordset[0], 'answer', { get: () => { throw new Error('row was read'); } });
  expect(() => decodeRows(reply, 10, 1000)).toThrow(new InvalidResultError('answer', typeName === 'Numeric' ? 'DECIMAL' : typeName.toUpperCase()));
});
