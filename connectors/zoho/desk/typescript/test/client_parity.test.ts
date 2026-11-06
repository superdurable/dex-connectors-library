// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { describe, expect, it } from 'vitest';
import { ZohoDeskClient } from '../src/client.js';
import { decodeObject } from '../src/ticket.js';
import type { CredentialSource, Credentials } from '../src/generated/models.js';

const call = { context: { cancellationSignal: new AbortController().signal } } as unknown as Call;
const credential = (authMethodId?: string): Credentials => ({
  ...(authMethodId ? { authMethodId } : {}), accessToken: new SecretString('synthetic-access'),
}) as Credentials;
const source = (authMethodId?: string): CredentialSource => ({
  resolve: async () => credential(authMethodId), resolveWithRefresh: async () => credential(authMethodId),
});

describe('Go Desk client parity', () => {
  it('requires an authorization method but not unused refresh fields', async () => {
    const missing = new ZohoDeskClient({ orgId: '2389290' }, source());
    expect(await missing.startSession(call, 'getTicket')).toMatchObject({ disposition: 'defect' });
    const valid = new ZohoDeskClient({ orgId: '2389290' }, source('zoho-us-oauth'));
    expect(await valid.startSession(call, 'getTicket')).toMatchObject({ baseURL: 'https://desk.zoho.com/api/v1' });
  });

  it('keeps HTTP 503 when the response body cannot be read', async () => {
    const client = new ZohoDeskClient({ orgId: '2389290' }, source('zoho-us-oauth'), {
      apiBaseURL: 'http://127.0.0.1:12345/api/v1',
      send: async request => { request.onResponseChunk?.(new Uint8Array(), 503, {}); throw new Error('body read failed'); },
    });
    const session = await client.startSession(call, 'getTicket');
    if ('disposition' in session) throw new Error('session did not start');
    expect(await client.exchange(call, 'getTicket', session, 'GET', '/tickets/1')).toMatchObject({ outcome: { disposition: 'unavailable', failure: { kind: 'AVAILABILITY' }, response: { status: 503 } } });
  });

  it('shares the operation signal across credential resolution and all requests', async () => {
    let credentialSignal: AbortSignal | undefined;
    const requestSignals: (AbortSignal | undefined)[] = [];
    const credentials: CredentialSource = {
      resolve: async () => credential('zoho-us-oauth'),
      resolveWithRefresh: async (_call, _driver, signal) => {
        credentialSignal = signal;
        return credential('zoho-us-oauth');
      },
    };
    const client = new ZohoDeskClient({ orgId: '2389290' }, credentials, {
      apiBaseURL: 'http://127.0.0.1:12345/api/v1',
      send: async request => { requestSignals.push(request.signal); return { status: 200, headers: {}, body: Buffer.from('{}') }; },
    });
    const session = await client.startSession(call, 'getTicket');
    if ('disposition' in session) throw new Error('session did not start');
    await client.exchange(call, 'getTicket', session, 'GET', '/tickets/1');
    await client.exchange(call, 'getTicket', session, 'GET', '/tickets/1/threads');
    expect(requestSignals).toEqual([credentialSignal, credentialSignal]);
  });

  it('does not expose malformed provider JSON in a failure message', () => {
    expect(() => decodeObject(Buffer.from('{"secret":"partial'))).toThrow('ticket response is not a ticket object');
  });

  it('requests five threads when getTicket omits threadLimit', async () => {
    const urls: string[] = [];
    const client = new ZohoDeskClient({ orgId: '2389290' }, source('zoho-us-oauth'), {
      apiBaseURL: 'http://127.0.0.1:12345/api/v1',
      send: async request => {
        urls.push(request.url);
        if (request.url.includes('/threads') || request.url.includes('/comments')) {
          return { status: 204, headers: {}, body: Buffer.alloc(0) };
        }
        return { status: 200, headers: {}, body: Buffer.from(JSON.stringify({
          id: '1892000000042034', status: 'Open', createdTime: '2026-01-28T09:00:00Z', modifiedTime: '2026-01-28T09:03:00Z',
        })) };
      },
    });
    const result = await client.getTicket().invoke(call, { ticketId: '1892000000042034' });
    expect(result).toMatchObject({ kind: 'branch', branch: 'found' });
    expect(new URL(urls[1]!).searchParams.get('limit')).toBe('5');
  });
});
