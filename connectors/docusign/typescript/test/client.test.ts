// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { escapePath, goQueryEscape, type DocuSignClient } from '../src/client.js';
import { CreateEnvelopeFromTemplateOperation, parseDocuSignTimestamp } from '../src/operations.js';
import type { Call } from '@superdurable/connectors-sdk';

describe('Go-compatible DocuSign wire values', () => {
  it('escapes path segments like Go url.PathEscape', () => {
    expect(escapePath("A!B'C(D)E*F,G;H?I/J ~ $&+=:@")).toBe('A%21B%27C%28D%29E%2AF%2CG%3BH%3FI%2FJ%20~%20$&+=:@');
  });
  it('encodes query components like Go url.QueryEscape', () => {
    const cases: ReadonlyArray<readonly [string, string]> = [
      ['a b~*+', 'a+b~%2A%2B'],
      ['AZaz09-._~', 'AZaz09-._~'],
      ["! '()*", '%21+%27%28%29%2A'],
      ['a&b=c/d?e', 'a%26b%3Dc%2Fd%3Fe'],
      ['é 雪', '%C3%A9+%E9%9B%AA'],
    ];
    for (const [value, escaped] of cases) expect(goQueryEscape(value)).toBe(escaped);
  });
  it('rejects invalid calendar dates and hours', () => {
    for (const value of ['2026-02-30T12:00:00Z', '2025-02-29T12:00:00Z', '2026-10-04T24:00:00Z']) {
      expect(() => parseDocuSignTimestamp(value)).toThrow();
    }
  });
  it('preserves nanoseconds, comma fractions, and large Go offsets', () => {
    expect(parseDocuSignTimestamp('2024-02-29T12:00:00,123456789Z')).toBe('2024-02-29T12:00:00.123456789Z');
    expect(parseDocuSignTimestamp('2026-10-04T12:00:00+99:59')).toBe('2026-09-30T08:01:00Z');
  });
});

describe('create checkpoint recovery', () => {
  it('preserves the create failure when read-back finds multiple envelopes', async () => {
    const original = { kind: 'AVAILABILITY', provider: 'docusign', operation: 'createEnvelopeFromTemplate', message: 'original create failure' };
    const envelope = { envelopeId: '8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a', status: 'sent', customFields: { textCustomFields: [{ name: 'dexIdempotencyKey', value: 'call-1' }] } };
    const client = {
      openSession: async () => ({}), currentTime: () => new Date('2026-10-04T12:00:00Z'), receipt: () => ({}),
      classify: () => ({ retry: true, retryAfterMs: 0, failure: original }),
      requestAPI: async (_call: Call, _session: unknown, method: string) => ({
        status: method === 'POST' ? 500 : 200, headers: {}, isTooLarge: false,
        body: Buffer.from(method === 'POST' ? '{}' : JSON.stringify({ envelopes: [envelope, envelope] })),
      }),
    } as unknown as DocuSignClient;
    const call = { id: 'call-1', idempotencyKey: 'call-1', context: {
      attempt: 1, firstAttemptAt: new Date('2026-10-04T12:00:00Z'), hasLastHeartbeatValue: () => false,
      recordHeartbeat: async () => {},
    } } as unknown as Call;
    const result = await new CreateEnvelopeFromTemplateOperation(client).invoke(call, {
      templateId: '8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a',
      templateRoles: [{ roleName: 'Customer', name: 'Priya Raman', email: 'priya@example.com' }],
    });
    expect(result.kind).toBe('uncertain');
    if (result.kind !== 'uncertain') throw new Error('expected uncertainty');
    expect(result.failure).toEqual(original);
  });
  it('records a timestamp with trimmed fractional zeros', async () => {
    let checkpoint: unknown;
    const client = {
      openSession: async () => ({}), currentTime: () => new Date('2026-10-04T12:00:00.120Z'), receipt: () => ({}),
      requestAPI: async (_call: Call, _session: unknown, method: string) => ({
        status: 201, headers: {}, body: Buffer.from('{"envelopeId":"8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a","status":"sent"}'), isTooLarge: false, method,
      }),
    } as unknown as DocuSignClient;
    const call = { id: 'call-1', idempotencyKey: 'call-1', context: {
      attempt: 1, firstAttemptAt: new Date('2026-10-04T12:00:00Z'), hasLastHeartbeatValue: () => false,
      recordHeartbeat: async (value: unknown) => { checkpoint = value; },
    } } as unknown as Call;
    await new CreateEnvelopeFromTemplateOperation(client).invoke(call, {
      templateId: '8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a',
      templateRoles: [{ roleName: 'Customer', name: 'Priya Raman', email: 'priya@example.com' }],
    });
    expect(checkpoint).toEqual({ isDispatched: true, dispatchedAt: '2026-10-04T12:00:00.12Z' });
  });
  it.each([undefined, { isDispatched: false, dispatchedAt: '2026-10-04T10:00:00Z' }, { isDispatched: true, dispatchedAt: '2026-10-04T11:00:00Z' }])('handles a cleared, false, or dispatched checkpoint: %j', async stored => {
    let heartbeat: unknown = { isDispatched: true, dispatchedAt: '2026-10-04T11:00:00Z' };
    const context = {
      attempt: 2,
      firstAttemptAt: new Date('2026-10-04T12:00:00Z'),
      deadline: undefined,
      hasLastHeartbeatValue: () => true,
      getLastHeartbeatValue: () => heartbeat,
      recordHeartbeat: async (value: unknown) => { heartbeat = value; },
    };
    await context.recordHeartbeat(undefined);
    if (stored !== undefined) await context.recordHeartbeat(stored);
    const methods: string[] = [];
    const fromDates: string[] = [];
    const client = {
      openSession: async () => ({}),
      currentTime: () => new Date('2026-10-04T12:00:00Z'),
      receipt: () => ({}),
      requestAPI: async (_call: Call, _session: unknown, method: string, _path: string, query?: Record<string, string>) => {
        methods.push(method);
        if (query?.from_date) fromDates.push(query.from_date);
        return { status: method === 'GET' ? 200 : 201, headers: {}, body: Buffer.from(method === 'GET' ? '{"envelopes":[]}' : '{"envelopeId":"8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a","status":"sent"}'), isTooLarge: false };
      },
    } as unknown as DocuSignClient;
    const call = { id: 'call-1', idempotencyKey: 'call-1', context } as unknown as Call;
    const result = await new CreateEnvelopeFromTemplateOperation(client).invoke(call, {
      templateId: '8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a',
      templateRoles: [{ roleName: 'Customer', name: 'Priya Raman', email: 'priya@example.com' }],
    });
    if (stored?.isDispatched) {
      expect(result.kind).toBe('uncertain');
      expect(methods).toEqual(['GET']);
      expect(fromDates).toEqual(['2026-10-04T10:00:00Z']);
    } else {
      expect(result.kind).toBe('branch');
      if (result.kind !== 'branch') throw new Error('expected created branch');
      expect(result.branch).toBe('created');
      expect(methods).toEqual(['GET', 'POST']);
      expect(fromDates).toEqual(['2026-10-04T11:00:00Z']);
    }
  });
});
