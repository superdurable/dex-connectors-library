// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { escapePath, goQueryEscape, type DocuSignClient } from '../src/client.js';
import { CreateEnvelopeFromTemplateOperation, parseDocuSignTimestamp } from '../src/operations.js';
import type { Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';

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

/** A DocuSign client that answers each POST from a script and every listing with no tagged envelope. */
function scriptedDocuSign(posts: Array<{ status: number; body: string } | Error>) {
  const methods: string[] = [];
  const client = {
    openSession: async () => ({}), currentTime: () => new Date('2026-10-04T12:00:00Z'), receipt: () => ({}),
    classify: (operation: string, response: { status: number }) => ({
      retry: response.status === 429 || response.status >= 500, notFound: false, retryAfterMs: 0, errorCode: '',
      failure: { kind: response.status === 429 ? 'RATE_LIMIT' : response.status >= 500 ? 'AVAILABILITY' : 'PROVIDER_REJECTION', provider: 'docusign', operation, message: `HTTP ${response.status}` },
    }),
    requestAPI: async (_call: Call, _session: unknown, method: string) => {
      methods.push(method);
      if (method === 'GET') return { status: 200, headers: {}, body: Buffer.from('{"envelopes":[]}'), isTooLarge: false };
      const answer = posts.shift();
      if (!answer) throw new Error('unexpected POST');
      if (answer instanceof Error) throw answer;
      return { status: answer.status, headers: {}, body: Buffer.from(answer.body), isTooLarge: false };
    },
  } as unknown as DocuSignClient;
  return { client, methods };
}

/** Run one attempt of a Step execution from the heartbeat its previous attempt left. */
async function runCreateAttempt(client: DocuSignClient, attempt: number, heartbeat: unknown) {
  let value = heartbeat;
  const call = { id: 'call-1', idempotencyKey: 'call-1', context: {
    attempt, firstAttemptAt: new Date('2026-10-04T12:00:00Z'), deadline: undefined,
    hasLastHeartbeatValue: () => value !== undefined, getLastHeartbeatValue: () => value,
    recordHeartbeat: async (recorded: unknown) => { value = recorded; },
  } } as unknown as Call;
  const result = await new CreateEnvelopeFromTemplateOperation(client).invoke(call, {
    templateId: '8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a',
    templateRoles: [{ roleName: 'Customer', name: 'Priya Raman', email: 'priya@example.com' }],
  });
  return { result, heartbeat: value };
}

describe('create send counting', () => {
  it('does not count rate-limited resends and reads back before each resend', async () => {
    const { client, methods } = scriptedDocuSign([
      { status: 500, body: '{}' }, { status: 429, body: '{}' }, { status: 429, body: '{}' },
      { status: 201, body: '{"envelopeId":"8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a","status":"sent"}' },
    ]);
    let heartbeat: unknown;
    for (const attempt of [1, 2, 3]) {
      const run = await runCreateAttempt(client, attempt, heartbeat);
      heartbeat = run.heartbeat;
      expect(run.result.kind).toBe('retry');
      // Every attempt after the unknown send reads back first.
      expect(methods.filter(method => method === 'GET')).toHaveLength(attempt);
    }
    expect(heartbeat).toMatchObject({ sendCount: 1 });
    const final = await runCreateAttempt(client, 4, heartbeat);
    expect(final.result).toMatchObject({ kind: 'branch', branch: 'created' });
    expect(methods.filter(method => method === 'GET')).toHaveLength(4);
    expect(methods.filter(method => method === 'POST')).toHaveLength(4);
  });
  it.each([
    ['an unusable summary', { status: 201, body: '{"envelopeId":' }],
    ['a body cut off after the headers', new ProviderTransportError(false, 201, {})],
  ] as const)('never resends a create accepted with %s', async (_name, answer) => {
    const { client, methods } = scriptedDocuSign([answer]);
    let run = await runCreateAttempt(client, 1, undefined);
    expect(run.result.kind).toBe('retry');
    expect(run.heartbeat).toMatchObject({ isAccepted: true });
    run = await runCreateAttempt(client, 2, run.heartbeat);
    expect(run.result.kind).toBe('retry');
    run = await runCreateAttempt(client, 3, run.heartbeat);
    expect(run.result.kind).toBe('uncertain');
    expect(methods.filter(method => method === 'POST')).toHaveLength(1);
    expect(methods.filter(method => method === 'GET')).toHaveLength(3);
  });
  it('retries a 503 without counting a send', async () => {
    const { client, methods } = scriptedDocuSign([{ status: 503, body: '{}' }]);
    const run = await runCreateAttempt(client, 1, undefined);
    expect(run.result.kind).toBe('retry');
    expect(methods).toEqual(['POST']);
    expect(run.heartbeat).toMatchObject({ isDispatched: false, sendCount: 0 });
  });
  it('selects uncertain for a refusal after a lost checkpoint', async () => {
    const { client, methods } = scriptedDocuSign([{ status: 400, body: '{}' }]);
    const run = await runCreateAttempt(client, 2, undefined);
    expect(run.result.kind).toBe('uncertain');
    expect(methods).toEqual(['GET', 'POST']);
  });
  it('records no send for a refused first send', async () => {
    const { client } = scriptedDocuSign([{ status: 400, body: '{}' }]);
    const run = await runCreateAttempt(client, 1, undefined);
    expect(run.result).toMatchObject({ kind: 'branch', branch: 'providerRejected' });
    expect(run.heartbeat).toMatchObject({ isDispatched: false, sendCount: 0 });
  });
});

describe('create checkpoint recovery', () => {
  it('selects uncertain without resending when read-back finds multiple envelopes', async () => {
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
    expect(result.failure).toMatchObject({ kind: 'CONFLICT' });
  });
  it.each([[1, 'retry'], [3, 'uncertain']] as const)('after an unknown send on attempt %i of a cap of 3, a read-back miss selects %s', async (attempt, expected) => {
    const original = { kind: 'AVAILABILITY', provider: 'docusign', operation: 'createEnvelopeFromTemplate', message: 'original create failure' };
    const methods: string[] = [];
    const client = {
      openSession: async () => ({}), currentTime: () => new Date('2026-10-04T12:00:00Z'), receipt: () => ({}),
      classify: () => ({ retry: true, retryAfterMs: 0, failure: original }),
      requestAPI: async (_call: Call, _session: unknown, method: string) => {
        methods.push(method);
        return { status: method === 'POST' ? 500 : 200, headers: {}, isTooLarge: false, body: Buffer.from(method === 'POST' ? '{}' : '{"envelopes":[]}') };
      },
    } as unknown as DocuSignClient;
    const call = { id: 'call-1', idempotencyKey: 'call-1', context: {
      attempt, firstAttemptAt: new Date('2026-10-04T12:00:00Z'), hasLastHeartbeatValue: () => false,
      recordHeartbeat: async () => {},
    } } as unknown as Call;
    const result = await new CreateEnvelopeFromTemplateOperation(client).invoke(call, {
      templateId: '8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a',
      templateRoles: [{ roleName: 'Customer', name: 'Priya Raman', email: 'priya@example.com' }],
    });
    expect(result.kind).toBe(expected);
    expect(result).toMatchObject({ failure: original });
    expect(methods).toEqual(attempt === 1 ? ['POST', 'GET'] : ['GET', 'POST', 'GET']);
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
    expect(checkpoint).toEqual({ isDispatched: true, dispatchedAt: '2026-10-04T12:00:00.12Z', attempt: 1, sendCount: 1, isAccepted: false });
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
    // Below the cap of 3, every later attempt reads back and then resends.
    expect(result.kind).toBe('branch');
    if (result.kind !== 'branch') throw new Error('expected created branch');
    expect(result.branch).toBe('created');
    expect(methods).toEqual(['GET', 'POST']);
    expect(fromDates).toEqual([stored?.isDispatched ? '2026-10-04T10:00:00Z' : '2026-10-04T11:00:00Z']);
    // The resend keeps the earliest dispatch time.
    if (stored?.isDispatched) expect(heartbeat).toEqual({ ...stored, attempt: 2, sendCount: 2, isAccepted: false });
  });
  it('selects uncertain without sending when earlier attempts reached the cap', async () => {
    let heartbeat: unknown = { isDispatched: true, dispatchedAt: '2026-10-04T11:00:00Z' };
    const methods: string[] = [];
    const client = {
      openSession: async () => ({}), currentTime: () => new Date('2026-10-04T12:00:00Z'), receipt: () => ({}),
      requestAPI: async (_call: Call, _session: unknown, method: string) => {
        methods.push(method);
        return { status: 200, headers: {}, body: Buffer.from('{"envelopes":[]}'), isTooLarge: false };
      },
    } as unknown as DocuSignClient;
    const call = { id: 'call-1', idempotencyKey: 'call-1', context: {
      attempt: 4, firstAttemptAt: new Date('2026-10-04T12:00:00Z'), deadline: undefined,
      hasLastHeartbeatValue: () => true, getLastHeartbeatValue: () => heartbeat,
      recordHeartbeat: async (value: unknown) => { heartbeat = value; },
    } } as unknown as Call;
    const result = await new CreateEnvelopeFromTemplateOperation(client).invoke(call, {
      templateId: '8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a',
      templateRoles: [{ roleName: 'Customer', name: 'Priya Raman', email: 'priya@example.com' }],
    });
    expect(result.kind).toBe('uncertain');
    expect(methods).toEqual(['GET']);
  });
});
