// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { once } from 'node:events';
import { createServer } from 'node:http';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { BrevoClient, isPlainEmail } from '../src/client.js';

const credentials = { resolve: async () => ({ apiKey: new SecretString('brevo-test-key') }) };

describe('Brevo client configuration', () => {
  it('rejects unsafe base URLs and nonpositive request limits before sending', () => {
    expect(() => new BrevoClient({ baseUrl: 'http://public.example/v3' }, credentials)).toThrow();
    expect(() => new BrevoClient({ maxResponseBytes: -1 }, credentials)).toThrow();
    expect(() => new BrevoClient({ timeoutMs: -1 }, credentials)).toThrow();
  });

  it('preserves a definitive rejection when its unused error body is oversized', async () => {
    const client = new BrevoClient({}, credentials, { send: async () => { throw new BodyTooLargeError(403); } });
    const call = { context: { cancellationSignal: new AbortController().signal } } as Call;
    const result = await client.createOrUpdateContact().invoke(call, { email: 'member@meridian.example' });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('providerRejected');
  });

  it('accepts plain email addresses and rejects display names', () => {
    expect(isPlainEmail('member+news@meridian.example')).toBe(true);
    expect(isPlainEmail('a&b=$c@meridian.example')).toBe(true);
    expect(isPlainEmail('müller@meridian.example')).toBe(true);
    expect(isPlainEmail('member..news@meridian.example')).toBe(false);
    expect(isPlainEmail('member@meridian.example@other.example')).toBe(false);
    expect(isPlainEmail('"a b"@meridian.example')).toBe(false);
    expect(isPlainEmail('Member <member@meridian.example>')).toBe(false);
    expect(isPlainEmail('member@meridian.example\napi-key: leaked')).toBe(false);
  });

  it('escapes contact lookup paths exactly like Go url.PathEscape', async () => {
    let requestUrl = '';
    const client = new BrevoClient({}, credentials, { send: async request => {
      requestUrl = request.url;
      return { status: 404, headers: {}, body: new Uint8Array() };
    } });
    const call = { context: { cancellationSignal: new AbortController().signal } } as Call;
    await client.getContact().invoke(call, { email: "a!b&c=$d+e'f@meridian.example" });
    expect(requestUrl).toBe('https://api.brevo.com/v3/contacts/a%21b&c=$d+e%27f@meridian.example');
  });

  it('retries when the local provider refuses a TCP connection', async () => {
    const server = createServer();
    server.listen(0, '127.0.0.1');
    await once(server, 'listening');
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('fixture has no TCP address');
    await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
    const client = new BrevoClient({ baseUrl: `http://127.0.0.1:${address.port}/v3` }, credentials);
    const call = { context: { cancellationSignal: new AbortController().signal } } as Call;
    const result = await client.getContact().invoke(call, { email: 'member@meridian.example' });
    expect(result.kind).toBe('retry');
    if (result.kind === 'retry') expect(result.failure.kind).toBe('TRANSPORT');
    const upsert = await client.createOrUpdateContact().invoke(call, { email: 'member@meridian.example' });
    expect(upsert.kind).toBe('retry');
    if (upsert.kind === 'retry') expect(upsert.failure.kind).toBe('TRANSPORT');
  });
});
