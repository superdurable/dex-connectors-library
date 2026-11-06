// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { describe, expect, it } from 'vitest';
import { ZohoDeskClient } from '../src/client.js';
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
});
