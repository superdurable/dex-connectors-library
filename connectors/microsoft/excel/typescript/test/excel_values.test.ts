// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { decodeColumns, decodeRange, wireCell } from '../src/excel_values.js';

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
