// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { AddressRefusal, ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { JiraServiceManagementClient, pathEscape, queryEscape } from '../src/client.js';

const connection = { provider: 'atlassian', name: 'jsm-test' };
const credentials = new StaticCredentialProvider([[connection, { accessToken: new SecretString('inert-access-token'), oauthClientId: 'inert-client-id', oauthClientSecret: new SecretString('inert-secret'), refreshToken: new SecretString('inert-refresh') }]]);
const call = { id: 'test-call', connection, idempotencyKey: '', context: { cancellationSignal: new AbortController().signal, attempt: 1 } };

describe('Atlassian request boundary', () => {
  it('matches Go URL path and query escaping', () => {
    const value = "a!$&+,:;=@/?'()*~ é";
    expect(pathEscape(value)).toBe('a%21$&+%2C:%3B=@%2F%3F%27%28%29%2A~%20%C3%A9');
    expect(queryEscape(value)).toBe('a%21%24%26%2B%2C%3A%3B%3D%40%2F%3F%27%28%29%2A~+%C3%A9');
  });

  it('retries an address refusal and a local connection failure before a read', async () => {
    for (const error of [new AddressRefusal('policy'), new ProviderTransportError(true)]) {
      const send = async (): Promise<never> => { throw error; };
      const client = new JiraServiceManagementClient({ endpoint: 'http://127.0.0.1:12345', cloudId: '11223344-a1b2-4b33-8c44-def123456789' }, credentials, { send });
      const result = await client.exchange(call as never, { token: 'inert-access-token', cloudID: '11223344-a1b2-4b33-8c44-def123456789' }, { method: 'GET', api: 'desk', path: '/request/ITH-12' });
      expect(client.read(call as never, 'getTicket', 'request', result).kind).toBe('retry');
      expect(result.isProvablyNotSent).toBe(true);
    }
  });

  it('selects one scoped site and rejects a malformed accessible-resources response', async () => {
    const cloudID = '11223344-a1b2-4b33-8c44-def123456789';
    const body = Buffer.from(JSON.stringify([{ id: cloudID, scopes: ['read:servicedesk-request'] }]));
    const client = new JiraServiceManagementClient({ endpoint: 'http://127.0.0.1:12345' }, credentials, { send: async () => ({ status: 200, headers: {}, body }) });
    expect(await client.start(call as never, 'getTicket')).toEqual({ token: 'inert-access-token', cloudID });
    const malformed = new JiraServiceManagementClient({ endpoint: 'http://127.0.0.1:12345' }, credentials, { send: async () => ({ status: 200, headers: {}, body: Buffer.from('[{"id":42}]') }) });
    expect(await malformed.start(call as never, 'getTicket')).toMatchObject({ kind: 'defect', failure: { kind: 'PROTOCOL' } });
  });

  it('classifies a large rate-limit response by its status', () => {
    const client = new JiraServiceManagementClient({ cloudId: '11223344-a1b2-4b33-8c44-def123456789' }, credentials);
    const result = { response: { status: 429, headers: {}, body: new Uint8Array(), isTooLarge: true, reflectedSecret: false } };
    expect(client.read(call as never, 'getTicket', 'request', result).kind).toBe('retry');
  });
});
