// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { CalendarClient, pathEscape } from '../src/client.js';
import { parseDate, parseInstant, validateEventTimes } from '../src/event.js';

const reference = { provider: 'google', name: 'calendar-unit' };
const input = {
  calendarId: 'primary', eventId: 'event12345', summary: 'Meeting',
  start: { dateTime: '2026-02-24T09:00:00-08:00', timeZone: 'America/Los_Angeles' },
  end: { dateTime: '2026-02-24T10:00:00-08:00', timeZone: 'America/Los_Angeles' },
};
const call: Call = {
  id: '01e84fae-1ee5-5e7d-97f5-add3b4d9b290', idempotencyKey: 'event12345', connection: reference,
  operation: { connectorId: 'google-calendar', operationId: 'createEvent' },
  context: { flowId: 'calendar-unit', cancellationSignal: new AbortController().signal } as Call['context'],
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
};

describe('Google-compatible input validation', () => {
  it('escapes path segments exactly like Go url.PathEscape', () => {
    expect(pathEscape("a/b !'()*+,;?~$&+=:@")).toBe('a%2Fb%20%21%27%28%29%2A+%2C%3B%3F~$&+=:@');
  });
  it('rejects impossible dates and 24-hour clocks while accepting Go offset and comma fractions', () => {
    expect(() => parseDate('2026-02-30', 'date')).toThrow();
    expect(() => parseDate('2025-02-29', 'date')).toThrow();
    expect(parseDate('2024-02-29', 'date')).toBeGreaterThan(0);
    expect(() => parseInstant('2026-02-24T24:00:00Z', 'dateTime')).toThrow();
    expect(Number.isFinite(parseInstant('2026-02-24T09:00:00,5+99:59', 'dateTime'))).toBe(true);
    expect(() => validateEventTimes(input.end, input.start)).toThrow();
  });
  it('matches bare Unicode and domain-literal addresses and rejects invalid dots', async () => {
    const client = new CalendarClient({}, new StaticCredentialProvider([[reference, { accessToken: new SecretString('unit-token') }]]), { send: async () => { throw new Error('unexpected provider call'); } });
    const valid = await client.createEvent().invoke(call, { ...input, attendees: [{ email: 'élise@例子.测试' }, { email: 'person@[127.0.0.1]' }] });
    expect(valid.kind).toBe('retry');
    const invalid = await client.createEvent().invoke(call, { ...input, attendees: [{ email: 'a..b@example.com' }] });
    expect(invalid.kind).toBe('branch');
    if (invalid.kind === 'branch') expect(invalid.branch).toBe('defect');
  });
});
