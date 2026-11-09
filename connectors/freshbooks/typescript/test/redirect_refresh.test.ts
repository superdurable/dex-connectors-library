// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { afterEach, expect, it, vi } from 'vitest';
import { SecretString, StaticCredentialProvider, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { FreshBooksClient } from '../src/client.js';
import type { Credentials } from '../src/generated/models.js';

afterEach(() => vi.unstubAllGlobals());

it('sends the registered callback and rotates the refresh token', async () => {
  const requests: Request[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
    requests.push(new Request(input, init));
    return new Response(JSON.stringify({ access_token: 'next-access', refresh_token: 'next-refresh',
      token_type: 'Bearer', expires_in: 3600, scope: 'user:profile:read user:invoices:read' }),
    { status: 200, headers: { 'Content-Type': 'application/json' } });
  });
  const client = new FreshBooksClient({ redirectUri: 'https://dex.example/callback' },
    new StaticCredentialProvider<Credentials>([]));
  const driver = (client as unknown as { refreshDriver: CredentialRefreshDriver<Credentials> }).refreshDriver;
  const credentials: Credentials = { oauthClientId: 'client-id', oauthClientSecret: new SecretString('client-secret'),
    accessToken: new SecretString('old-access'), refreshToken: new SecretString('old-refresh') };
  const result = await driver.refresh({ credentials, now: new Date() }, new AbortController().signal);
  expect(result.credentials.accessToken.reveal()).toBe('next-access');
  expect(result.credentials.refreshToken.reveal()).toBe('next-refresh');
  expect(requests).toHaveLength(1);
  expect(requests[0]!.url).toBe('https://api.freshbooks.com/auth/oauth/token');
  const body = await requests[0]!.json() as Record<string, string>;
  expect(body.redirect_uri).toBe('https://dex.example/callback');
  expect(body.grant_type).toBe('refresh_token');
});
