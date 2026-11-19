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
  it('encodes query parameters in Go key order and escape format', async () => {
    const urls: string[] = [];
    const client = new SendGridClient({}, credentials, { send: async request => {
      urls.push(request.url);
      return { status: 200, headers: {}, body: Buffer.from(request.url.includes('/v3/stats?') ? '[]' : '{"result":[],"_metadata":{"count":0}}') };
    } });
    await client.getStats().invoke(call, { startDate: '2026-01-01', endDate: '2026-01-31' });
    await client.listTemplates().invoke(call, { pageToken: 'a*b~c d' });
    expect(urls).toEqual([
      'https://api.sendgrid.com/v3/stats?end_date=2026-01-31&start_date=2026-01-01',
      'https://api.sendgrid.com/v3/templates?generations=dynamic&page_size=100&page_token=a%2Ab~c+d',
    ]);
  });
});
