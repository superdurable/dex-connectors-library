// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { Client } from '../src/client.js';

const call = { context: { cancellationSignal: new AbortController().signal }, id: 'test-call' } as Call;
const credentials = { resolve: async () => ({ accessToken: new SecretString('test-token') }) };

describe('Square client', () => {
  it('uses configured timeout and Go path escaping', async () => {
    const requests: Array<{ url: string; timeoutMs?: number }> = [];
    const client = new Client({ baseUrl: 'http://127.0.0.1:28710/v2', timeoutMs: 1450 }, credentials, {
      send: async request => { requests.push({ url: request.url, timeoutMs: request.timeoutMs }); return { status: 200, headers: {}, body: Buffer.from('{"payment":{"id":"pay_1"}}') }; },
    });
    const result = await client.getPayment().invoke(call, { paymentId: "a!$&+,:;=@b" });
    expect(result.kind).toBe('branch');
    expect(requests).toEqual([{ url: 'http://127.0.0.1:28710/v2/payments/a%21$&+%2C:%3B=@b', timeoutMs: 1450 }]);
  });
  it('classifies an oversized 429 by status before body size', async () => {
    const client = new Client({ baseUrl: 'http://127.0.0.1:28710/v2', maxResponseBytes: 1 }, credentials, {
      send: async () => ({ status: 429, headers: { 'retry-after': '2' }, body: Buffer.alloc(100_000) }),
    });
    const result = await client.listPayments().invoke(call, {});
    expect(result.kind).toBe('retry');
    if (result.kind === 'retry') expect(result.failure.kind).toBe('RATE_LIMIT');
  });
});
