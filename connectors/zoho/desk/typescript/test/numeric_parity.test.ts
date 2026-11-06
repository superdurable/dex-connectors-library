// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { count, decodeObject, decodeTicket, timestamp } from '../src/ticket.js';

const ticket = (id: string) => decodeTicket(decodeObject(Buffer.from(`{"id":${id},"status":"Open","createdTime":"2026-01-02T03:04:05Z","modifiedTime":"2026-01-02T03:04:05Z"}`)));

describe('Go JSON number and timestamp parity', () => {
  it('retains a numeric ID beyond JavaScript safe integer precision', () => {
    expect(ticket('9007199254740993').ticket.id).toBe('9007199254740993');
  });
  it.each(['5.0', '1e3', '-1'])('rejects non-integer count spelling %s', raw => {
    const value = decodeObject(Buffer.from(`{"count":${raw}}`)).count;
    expect(() => count(value)).toThrow();
  });
  it('accepts Go strconv.Atoi string forms and negative zero', () => {
    expect(count('+1')).toBe(1);
    expect(count('-0')).toBe(0);
    expect(count(decodeObject(Buffer.from('{"count":-0}')).count)).toBe(0);
  });
  it.each(['5.0', '1e3'])('rejects non-integer ID spelling %s', raw => {
    expect(() => ticket(raw)).toThrow();
  });
  it('rejects an hour of 24', () => {
    expect(() => timestamp('createdTime', '2026-01-02T24:00:00Z')).toThrow('createdTime');
  });
});
