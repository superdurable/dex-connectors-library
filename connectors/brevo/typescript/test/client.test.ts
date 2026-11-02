// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
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
    expect(isPlainEmail('Member <member@meridian.example>')).toBe(false);
    expect(isPlainEmail('member@meridian.example\napi-key: leaked')).toBe(false);
  });
});
