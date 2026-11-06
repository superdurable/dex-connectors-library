// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { goPathEscape, SpreadsheetClient } from '../src/client.js';

const credential = { accessToken: new SecretString('fixture-token'), oauthClientId: 'fixture-client', oauthClientSecret: new SecretString('fixture-secret'), refreshToken: new SecretString('fixture-refresh') };
const call = { id: 'f1997f37-cb39-569a-bd4a-241d6f8da859', operation: { operationId: 'getValues' }, context: { cancellationSignal: new AbortController().signal } } as Call;

describe('Google Sheets client', () => {
  it('escapes exactly Go path component reserved bytes', () => {
    expect(goPathEscape("A z!'/~$&+=:@,;?()é")).toBe('A%20z%21%27%2F~$&+=:@%2C%3B%3F%28%29%C3%A9');
  });

  it('rejects an insecure remote endpoint and invalid limits', () => {
    const source = { resolve: async () => credential, resolveWithRefresh: async () => credential };
    expect(() => new SpreadsheetClient({ endpoint: 'http://sheets.example.test' }, source)).toThrow();
    expect(() => new SpreadsheetClient({ endpoint: 'http://127.1' }, source)).toThrow();
    expect(() => new SpreadsheetClient({ maxRows: 1 }, source)).toThrow();
  });

  it('rejects wrong-typed provider fields and enforces the row limit', () => {
    const client = new SpreadsheetClient({ maxRows: 2 }, { resolve: async () => credential, resolveWithRefresh: async () => credential });
    const exchange = { status: 200, headers: {}, body: Buffer.from('{"range":4,"values":[]}'), requestId: '', isTooLarge: false, isProvablyNotSent: false };
    expect(client.decodeValues(exchange, 'getValues').failure?.kind).toBe('PROTOCOL');
    expect(client.decodeValues({ ...exchange, body: Buffer.from('{"values":[[],[],[]]}') }, 'getValues').failure?.kind).toBe('RESPONSE_TOO_LARGE');
    expect(client.receipt(call).provider).toBe('google-sheets');
  });
});

describe('Interrupted provider response', () => {
  it('retains a rejected append status after a body read error', async () => {
    const client = new SpreadsheetClient({}, { resolve: async () => credential, resolveWithRefresh: async () => credential }, {
      send: async request => {
        request.onResponseChunk?.(Buffer.from('{'), 503, { 'x-goog-request-id': 'safe-request-id' });
        throw new Error('body interrupted');
      },
    });
    const result = await client.request(call, credential, 'POST', 'sheet', '/values/append', {}, {}, true);
    expect(result.exchange).toMatchObject({ status: 503, requestId: 'safe-request-id' });
  });
});
