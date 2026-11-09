// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { afterEach, expect, it, vi } from 'vitest';
import { SecretString, StaticCredentialProvider, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { CreatioClient } from '../src/client.js';
import type { Credentials } from '../src/generated/models.js';

afterEach(() => vi.unstubAllGlobals());

it('mints at the configured Creatio Identity Service endpoint', async () => {
  const requests: Request[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
    requests.push(new Request(input, init));
    return new Response(JSON.stringify({ access_token: 'next-access', token_type: 'Bearer', expires_in: 3600 }),
      { status: 200, headers: { 'Content-Type': 'application/json' } });
  });
  const client = new CreatioClient({ baseUrl: 'https://app.example.com', tokenEndpoint: 'https://identity.example.com/connect/token' },
    new StaticCredentialProvider<Credentials>([]));
  const driver = (client as unknown as { refreshDriver: CredentialRefreshDriver<Credentials> }).refreshDriver;
  const credentials: Credentials = { clientId: 'sample-client-id', clientSecret: new SecretString('sample-client-secret'), accessToken: new SecretString('') };
  const result = await driver.refresh({ credentials, now: new Date() }, new AbortController().signal);
  expect(result.credentials.accessToken?.reveal()).toBe('next-access');
  expect(requests).toHaveLength(1);
  expect(requests[0]!.url).toBe('https://identity.example.com/connect/token');
  const body = await requests[0]!.text();
  expect(new URLSearchParams(body).get('grant_type')).toBe('client_credentials');
  expect(new URLSearchParams(body).get('client_id')).toBe('sample-client-id');
  expect(new URLSearchParams(body).get('client_secret')).toBe('sample-client-secret');
});
