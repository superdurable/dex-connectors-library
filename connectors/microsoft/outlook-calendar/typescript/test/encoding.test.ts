// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
import { expect, it } from "vitest";
import { graphQuery, pathEscape } from "../src/http.js";
import { instant, returnedBoundary } from "../src/event.js";
it.each([
  ["a b", "a=a%20b&z=a%20b", "a%20b"],
  ["x~*!'()", "a=x~%2A%21%27%28%29&z=x~%2A%21%27%28%29", "x~%2A%21%27%28%29"],
  [
    "calendar/id?=x+y",
    "a=calendar%2Fid%3F%3Dx%2By&z=calendar%2Fid%3F%3Dx%2By",
    "calendar%2Fid%3F=x+y",
  ],
  ["é", "a=%C3%A9&z=%C3%A9", "%C3%A9"],
])("matches Go query and path escaping for %s", (value, query, path) => {
  expect(graphQuery({ z: value, a: value })).toBe(query);
  expect(pathEscape(value)).toBe(path);
});
it.each([
  "2026-02-24T24:00:00Z",
  "2026-02-24T17:00:00",
])("rejects non-round-trippable time %s", (value) =>
  expect(() => instant("start.dateTime", value)).toThrow(/start.dateTime/),
);
it("accepts zero seventh fractional digit", () =>
  expect(
    returnedBoundary(
      "start",
      { dateTime: "2026-02-24T17:00:00.0000000", timeZone: "UTC" },
      "UTC",
    ).value.dateTime,
  ).toBe("2026-02-24T17:00:00Z"));

it('accepts Go nanosecond instants and formats returned times at whole seconds', () => {
  expect(instant('start.dateTime', '2026-02-24T17:00:00.0000001Z')).toBe(Date.parse('2026-02-24T17:00:00Z'));
  expect(returnedBoundary('start', { dateTime: '2026-02-24T17:00:00.1234567', timeZone: 'UTC' }, 'UTC').value.dateTime)
    .toBe('2026-02-24T17:00:00Z');
});

it('omits empty optional fields as the Go event JSON does', async () => {
  const { event } = await import('../src/event.js');
  const decoded = event({ id: 'event1', start: { dateTime: '2026-02-24T17:00:00', timeZone: 'UTC' },
    end: { dateTime: '2026-02-24T18:00:00', timeZone: 'UTC' }, body: { content: '' },
    location: { displayName: '' }, organizer: {}, isOnlineMeeting: true, onlineMeeting: { joinUrl: '' } }, '', 'UTC');
  expect(decoded).not.toHaveProperty('body');
  expect(decoded).not.toHaveProperty('location');
  expect(decoded).not.toHaveProperty('organizerEmail');
  expect(decoded.onlineMeeting).not.toHaveProperty('joinUrl');
});

it('rejects a repeated all-day midnight in America/Havana', async () => {
  const { validateEventTimes } = await import('../src/event.js');
  expect(() => validateEventTimes(
    { dateTime: '2026-11-01T00:00:00-04:00', timeZone: 'America/Havana' },
    { dateTime: '2026-11-02T00:00:00-05:00', timeZone: 'America/Havana' }, true,
  )).toThrow(/ambiguous midnight/);
});

it('rejects zone aliases Intl accepts but Go time.LoadLocation does not', async () => {
  const { validateZone } = await import('../src/event.js');
  expect(() => validateZone('timeZone', '+05:00')).toThrow();
  expect(() => validateZone('timeZone', 'america/los_angeles')).toThrow();
});

it('rejects bare addresses Go mail.ParseAddress rejects', async () => {
  const { validateAddress } = await import('../src/event.js');
  expect(() => validateAddress('a(c)@x.com')).toThrow();
  expect(() => validateAddress('a..b@x.com')).toThrow();
});

it('treats a Graph free-busy error:null as no schedule error', async () => {
  const { Client } = await import('../src/client.js');
  const { SecretString, StaticCredentialProvider } = await import('@superdurable/connectors-sdk');
  const { HeartbeatContext, scenarioCall } = await import('../../../../../conformance/runners/typescript/connector_scenarios.js');
  const connection = { provider: 'microsoft', name: 'outlook-calendar-scheduler' };
  const credentials = { authMethodId: 'microsoft-oauth', clientId: 'client', clientSecret: new SecretString('secret'),
    accessToken: new SecretString('token'), refreshToken: new SecretString('refresh') };
  const client = new Client({}, new StaticCredentialProvider([[connection, credentials]]), {
    baseURL: 'http://127.0.0.1:12345/v1.0', transport: async () => ({ status: 200, headers: {},
      body: Buffer.from('{"value":[{"scheduleId":"person@example.com","availabilityView":"0","scheduleItems":[],"error":null}]}') }),
  });
  const call = scenarioCall({ callId: 'calendar-null-error', operation: 'queryFreeBusy' } as never,
    connection, 'outlook-calendar', new HeartbeatContext(undefined));
  const result = await client.queryFreeBusy().invoke(call as never, { schedules: ['person@example.com'],
    timeMin: '2026-02-24T17:00:00Z', timeMax: '2026-02-24T18:00:00Z' });
  expect(result).toMatchObject({ kind: 'branch', branch: 'queried', value: { schedules: [{ isAvailabilityKnown: true }] } });
});

it('checks raw Graph next-link host and path before WHATWG normalization', async () => {
  const { validateNextLink } = await import('../src/http.js');
  for (const value of [
    'https://graph.microsoft.com:443/v1.0/me/calendarView',
    'https://GRAPH.microsoft.com/v1.0/me/calendarView',
  ]) expect(() => validateNextLink(value)).toThrow();
});
