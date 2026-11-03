// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { SendGridClient } from '../src/index.js';

const call = { id: 'sendgrid-test', context: { cancellationSignal: new AbortController().signal } } as Call;
const credentials = { resolve: async () => ({ apiKey: new SecretString('test-key-123') }) };

describe('SendGrid operations', () => {
  it('rejects invalid dates before contacting the provider', async () => {
    let requests = 0;
    const client = new SendGridClient({}, credentials, { send: async () => { requests++; throw new Error('unexpected request'); } });
    const result = await client.getStats().invoke(call, { startDate: '2026-02-30' });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('defect');
    expect(requests).toBe(0);
  });
  it('keeps missing credentials terminal', async () => {
    const client = new SendGridClient({}, { resolve: async () => { throw new Error('missing'); } });
    const result = await client.listTemplates().invoke(call, {});
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') { expect(result.branch).toBe('defect'); expect(result.failure?.kind).toBe('AUTHENTICATION'); }
  });
});
