// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { decodeColumns, decodeRange, parseA1Range, wireCell } from '../src/excel_values.js';

const bytes = (value: string): Uint8Array => new TextEncoder().encode(value);

it.each(['5.0', '1e3', '9007199254740992'])('rejects non-Go rowCount integer literal %s', literal => {
  expect(() => decodeRange(bytes(`{"rowCount":${literal},"columnCount":1,"values":[]}`), false)).toThrow();
});

it.each(['5.0', '1e3', '9007199254740992'])('rejects non-Go column index literal %s', literal => {
  expect(() => decodeColumns(bytes(`{"value":[{"id":"c1","index":${literal},"name":"Key"}]}`))).toThrow();
});

it('refuses absent and nonfinite literal cells', () => {
  expect(() => wireCell(null)).toThrow();
  expect(() => wireCell(Number.NaN)).toThrow();
  expect(() => wireCell(Number.POSITIVE_INFINITY)).toThrow();
  expect(wireCell('=SUM(A1:A3)')).toBe("'=SUM(A1:A3)");
});

it.each(['1e16', '6.02e23'])('accepts Go float64 cell %s', literal => {
  expect(decodeRange(bytes(`{"rowCount":1,"columnCount":1,"values":[[${literal}]]}`), false).values[0]![0])
    .toBe(Number(literal));
});

it.each(['aß1', 'ﬀ1'])('rejects non-ASCII A1 case folding %s', address => {
  expect(() => parseA1Range(address)).toThrow();
});

it.each([['ı1', 'I1'], ['ſ1', 'S1']])('accepts Go simple uppercase A1 mapping %s', (address, expected) => {
  expect(parseA1Range(address)).toEqual({ address: expected, rowCount: 1, columnCount: 1 });
});

it('matches Go TrimSpace around A1 addresses', () => {
  expect(parseA1Range('A1\u0085').address).toBe('A1');
  expect(() => parseA1Range('\uFEFFA1')).toThrow();
});

it('does not call an array cell Graph field 0 when a number overflows', () => {
  expect(() => decodeRange(bytes('{"rowCount":1,"columnCount":1,"values":[[1e999]]}'), false))
    .toThrow(/Graph number is not finite/);
});
