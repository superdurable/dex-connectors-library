// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { HelpScoutClient } from '../src/client.js';
import { decodeConversation, parseTimestamp } from '../src/decode.js';
import { Authorization, CredentialFields } from '../src/generated/refresh.js';
import type { Credentials } from '../src/generated/models.js';

const connection = { provider: 'helpscout', name: 'unit-test' };
const credentials: Credentials = { appId: 'inert-app-id', appSecret: new SecretString('inert-app-secret'), accessToken: new SecretString('inert-access-token') };

describe('Go-compatible provider values', () => {
  it('rejects impossible civil dates and accepts Go offset and comma fractions', () => {
    expect(() => parseTimestamp('2026-02-30T12:00:00Z')).toThrow();
    expect(() => parseTimestamp('2025-02-29T12:00:00Z')).toThrow();
    expect(() => parseTimestamp('2026-11-04T24:00:00Z')).toThrow();
    expect(parseTimestamp('2024-02-29T12:00:00,250+99:59')).toBe('2024-02-25T08:01:00.25Z');
    expect(parseTimestamp('0001-01-01T00:00:00Z')).toBe('0001-01-01T00:00:00Z');
    expect(parseTimestamp('0000-02-29T00:00:00Z')).toBe('0000-02-29T00:00:00Z');
  });

  it('rejects wrong-typed provider fields instead of coercing them', () => {
    expect(() => decodeConversation({ id: 501, status: 3 })).toThrow();
    expect(() => decodeConversation({ id: 501, tags: 'billing' })).toThrow();
  });

  it('keeps HTTP status classification when an error body exceeds the limit', async () => {
    const client = new HelpScoutClient({}, new StaticCredentialProvider([[connection, credentials]]), {
      send: async () => { throw new BodyTooLargeError(429, { 'x-ratelimit-retry-after': '3' }); },
    });
    const call = { id: '72c79694-b3a3-4ca8-b75c-b8e46b929d15', connection,
      operation: { connectorId: 'helpscout', operationId: 'searchConversations' },
      context: { cancellationSignal: new AbortController().signal } } as Call;
    const result = await client.searchConversations().invoke(call, {});
    expect(result.kind).toBe('retry');
    if (result.kind === 'retry') { expect(result.failure.kind).toBe('RATE_LIMIT'); expect(result.retryAfterMs).toBe(3000); }
  });
});

describe('generated Help Scout client credentials declaration', () => {
  it('posts the declared grant and retains an expiry', async () => {
    const requests: Array<{ url: string; init: RequestInit }> = [];
    const driver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Help Scout', auth: Authorization, credentialFields: CredentialFields,
      fetch: async (input, init) => { requests.push({ url: String(input), init: init ?? {} }); return Response.json({ access_token: 'new-token', token_type: 'Bearer', expires_in: 7200 }); } });
    const state = { credentials: { ...credentials, accessToken: new SecretString('') }, now: new Date('2026-11-04T12:00:00Z') };
    expect(driver.refreshRequired(state)).toBe(true);
    const replacement = await driver.refresh(state, new AbortController().signal);
    expect(requests).toHaveLength(1);
    expect(requests[0]!.url).toBe('https://api.helpscout.net/v2/oauth2/token');
    const form = new URLSearchParams(String(requests[0]!.init.body));
    expect(Object.fromEntries(form)).toEqual({ grant_type: 'client_credentials', client_id: 'inert-app-id', client_secret: 'inert-app-secret' });
    expect(replacement.credentials.accessToken?.reveal()).toBe('new-token');
    expect(replacement.expiresAt.getTime()).toBeGreaterThan(state.now.getTime());
  });

  it('marks invalid_client terminal while a server error remains retryable', async () => {
    let status = 400;
    const driver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Help Scout', auth: Authorization, credentialFields: CredentialFields,
      fetch: async () => Response.json(status === 400 ? { error: 'invalid_client' } : { error: 'server_error' }, { status }) });
    const state = { credentials, expiresAt: new Date(0), now: new Date() };
    await expect(driver.refresh(state, new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    status = 503;
    await expect(driver.refresh(state, new AbortController().signal)).rejects.not.toBeInstanceOf(ReauthorizationRequiredError);
  });
});
