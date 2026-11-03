// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ResendClient } from '../src/client.js';

const call = { id: 'test-call', idempotencyKey: 'test-call', context: {} } as Call;
const emailInput = { from: 'Acme <hello@example.test>', to: ['reader@example.test'], subject: 'Welcome', html: '<p>Hello</p>' };

function clientWith(send: NonNullable<NonNullable<ConstructorParameters<typeof ResendClient>[2]>['send']>, key = 'test-secret-token') {
  return new ResendClient({ baseUrl: 'http://127.0.0.1' }, { resolve: async () => ({ apiKey: new SecretString(key) }) }, { send });
}

describe('Resend request boundaries', () => {
  it('escapes a provider ID like Go url.PathEscape', async () => {
    let path = '';
    const client = clientWith(async request => {
      path = new URL(request.url).pathname;
      return { status: 200, headers: {}, body: Buffer.from('{"id":"mail"}') };
    });
    const result = await client.getEmail().invoke(call, { id: "a!cé$&+,:=@/" });
    expect(result.kind).toBe('branch');
    expect(path).toBe('/emails/a%21c%C3%A9$&+%2C:=@%2F');
  });

  it('classifies missing credentials as a terminal defect without calling the provider', async () => {
    let requests = 0;
    const client = clientWith(async () => { requests++; throw new Error('unexpected request'); }, ' ');
    const result = await client.sendEmail().invoke(call, emailInput);
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') { expect(result.branch).toBe('defect'); expect(result.failure?.kind).toBe('AUTHENTICATION'); }
    expect(requests).toBe(0);
  });

  it('retries a refused connection with the same call ID', async () => {
    const keys: string[] = [];
    const client = clientWith(async request => { keys.push(request.headers?.['Idempotency-Key'] ?? ''); throw new Error('connection refused'); });
    const result = await client.sendEmail().invoke(call, emailInput);
    expect(result.kind).toBe('retry');
    if (result.kind === 'retry') expect(result.failure.kind).toBe('AVAILABILITY');
    expect(keys).toEqual(['test-call']);
  });
});
