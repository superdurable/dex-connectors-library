// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { encodeGoQuery, escapeGoPath } from '../src/client.js';
import { buildFindRecordsQuery } from '../src/coql.js';
import { decodeModuleFields, decodeRecordPage, parseZohoTime } from '../src/records.js';

const bytes = (value: unknown) => Buffer.from(JSON.stringify(value));

describe('Go wire parity at provider boundaries', () => {
  it('uses Go path and query escaping for punctuation and Unicode', () => {
    expect(escapeGoPath("!$&'()*+,;=:@?/é")).toBe('%21$&%27%28%29%2A+%2C%3B=:@%3F%2F%C3%A9');
    expect(encodeGoQuery({ z: ['a b~*!'], a: ['x/y'] })).toBe('a=x%2Fy&z=a+b~%2A%21');
  });

  it('accepts leap days and year zero but rejects impossible COQL dates', () => {
    const input = { module: 'Contacts', fields: ['Email'], conditions: [{ field: 'Created_Time', operator: '=', value: { kind: 'date', text: '0000-02-29' } }] };
    expect(buildFindRecordsQuery(input)).toContain("'0000-02-29'");
    expect(() => buildFindRecordsQuery({ ...input, conditions: [{ ...input.conditions[0]!, value: { kind: 'date', text: '2026-02-29' } }] })).toThrow();
    expect(parseZohoTime('2026-02-30T00:00:00+00:00')).toBeUndefined();
    expect(parseZohoTime('2026-01-01T24:00:00+00:00')).toBeUndefined();
  });

  it('rejects wrong-typed provider field metadata instead of dropping it', () => {
    for (const field of [
      { api_name: 'Email', data_type: 'email', custom_field: 'false' },
      { api_name: 'Email', data_type: 'email', pick_list_values: 'bad' },
      { api_name: 'Email', data_type: 'email', lookup: { module: { api_name: 3 } } },
    ]) expect(() => decodeModuleFields(bytes({ fields: [field] }))).toThrow();
  });

  it('keeps Go null collection semantics for record pages', () => {
    expect(decodeRecordPage('Contacts', bytes({ data: null, info: { more_records: null } }))).toEqual({ records: [], hasMore: false });
  });
});
