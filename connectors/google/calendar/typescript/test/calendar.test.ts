// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, CredentialProvider, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { CalendarClient, pathEscape } from '../src/client.js';
import { CredentialMaterialError, decodeCredentials } from '../src/generated/models.js';
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

describe('credential resolve failures', () => {
  it.each([
    { name: 'reauthorization', failure: new ReauthorizationRequiredError(), isTerminal: true },
    { name: 'object not found', failure: new ObjectNotFoundError(), isTerminal: true },
    { name: 'connection not selected', failure: new ConnectionNotSelectedError(), isTerminal: true },
    { name: 'broker rejected', failure: new CredentialBrokerRejectedError(), isTerminal: true },
    { name: 'malformed material', failure: new CredentialMaterialError('stored credential is malformed'), isTerminal: true },
    { name: 'broker unavailable', failure: new CredentialBrokerUnavailableError(), isTerminal: false },
    { name: 'static connection not configured', failure: new Error('connection is not configured'), isTerminal: false },
  ])('selects defect only for a terminal $name failure', async ({ failure, isTerminal }) => {
    const client = new CalendarClient({}, { resolve: async () => { throw failure; } } as never, { send: async () => { throw new Error('unexpected provider call'); } });
    const result = await client.createEvent().invoke(call, input);
    expect(result).toMatchObject(isTerminal ? { kind: 'branch', branch: 'defect' } : { kind: 'retry' });
  });

  it('selects defect for malformed stored material behind the released SDK provider', async () => {
    const store = {
      scope: { projectId: 'project-1', kind: 'live' },
      readCredentialMaterial: async () => ({ material: { credentials: { unexpected: 'value' } }, connection: {} }),
    };
    const provider = new CredentialProvider(store as never, { connectorId: 'google-calendar', connectionName: 'calendar-unit' }, decodeCredentials);
    const client = new CalendarClient({}, provider as never, { send: async () => { throw new Error('unexpected provider call'); } });
    expect(await client.createEvent().invoke(call, input)).toMatchObject({ kind: 'branch', branch: 'defect' });
  });
});

describe('createEvent after an earlier attempt', () => {
  it.each([
    { attempt: 1, expected: { kind: 'branch', branch: 'conflict' } },
    { attempt: 2, expected: { kind: 'retry' } },
  ])('treats a taken but unreadable event ID by attempt ($attempt)', async ({ attempt, expected }) => {
    const credentials = new StaticCredentialProvider([[reference, { accessToken: new SecretString('token') }]]);
    const send = async (request: { method: string }) => ({ status: request.method === 'POST' ? 409 : 404, headers: {}, body: Buffer.from('{}') });
    const client = new CalendarClient({}, credentials as never, { send } as never);
    const attemptCall = { ...call, context: { ...call.context, attempt } } as Call;
    expect(await client.createEvent().invoke(attemptCall, input)).toMatchObject(expected);
  });
});
