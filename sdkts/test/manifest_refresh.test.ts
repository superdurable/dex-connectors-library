// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { generateKeyPairSync } from 'node:crypto';
import { describe, expect, it } from 'vitest';
import { SecretString } from '../src/credentials.js';
import { ReauthorizationRequiredError } from '../src/errors.js';
import { ManifestCredentialRefreshDriver } from '../src/oauthtoken/index.js';

interface Credentials {
  accessToken: SecretString;
  refreshToken: SecretString;
  clientId: string;
  clientSecret: SecretString;
  instanceUrl: string;
  authMethodId: string;
}

const auth = {
  defaultMethod: 'oauth', methods: [{
    id: 'oauth', type: 'oauth2', fields: [
      { name: 'access_token', type: 'secretString' }, { name: 'refresh_token', type: 'secretString' },
      { name: 'client_id', type: 'string' }, { name: 'client_secret', type: 'secretString' },
      { name: 'instance_url', type: 'url' },
    ],
    oauth2: {
      tokenEndpoint: 'https://oauth.example.test/token', tokenEndpointAuthMethod: 'client_secret_basic',
      tokenRequestEncoding: 'json', clientIDCredential: 'client_id', clientSecretCredential: 'client_secret',
      scopes: ['offline_access', 'files.read'], credentialMappings: [
        { credential: 'access_token', source: 'access_token' },
        { credential: 'refresh_token', source: 'refresh_token' },
        { credential: 'instance_url', source: 'instance_url' },
      ], refresh: { terminalErrorCodes: ['invalid_grant', 'invalid_client'], additionalParameters: { scope: 'offline_access files.read' } },
    },
  }, { id: 'static', type: 'apiKey', fields: [] }],
};
const credentialFields = { access_token: 'accessToken', refresh_token: 'refreshToken', client_id: 'clientId', client_secret: 'clientSecret', instance_url: 'instanceUrl' };
const credentials = (): Credentials => ({
  accessToken: new SecretString('old-access'), refreshToken: new SecretString('old-refresh'),
  clientId: 'client-id', clientSecret: new SecretString('client-secret'), instanceUrl: 'old-instance', authMethodId: 'oauth',
});
const state = () => ({ credentials: credentials(), now: new Date(), expiresAt: new Date(0) });

describe('manifest credential refresh', () => {
  it('uses declared request shape and rotates mapped fields only', async () => {
    const requests: RequestInit[] = [];
    const fetcher: typeof fetch = async (input, init) => {
      expect(String(input)).toBe('https://oauth.example.test/token');
      requests.push(init ?? {});
      return Response.json({ access_token: 'next-access', refresh_token: 'next-refresh', expires_in: 3600, token_type: 'Bearer', scope: 'files.read', instance_url: 'new-instance' });
    };
    const driver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth, credentialFields, fetch: fetcher });
    expect(driver.refreshRequired(state())).toBe(true);
    const result = await driver.refresh(state(), new AbortController().signal);
    expect(result.credentials.accessToken.reveal()).toBe('next-access');
    expect(result.credentials.refreshToken.reveal()).toBe('next-refresh');
    expect(result.credentials.clientSecret.reveal()).toBe('client-secret');
    expect(result.credentials.instanceUrl).toBe('new-instance');
    expect(result.expiresAt.getTime()).toBeGreaterThan(Date.now() + 59 * 60_000);
    expect(requests).toHaveLength(1);
    expect(JSON.parse(String(requests[0]?.body))).toMatchObject({ grant_type: 'refresh_token', refresh_token: 'old-refresh', scope: 'offline_access files.read' });
    expect(String((requests[0]?.headers as Record<string, string>).Authorization)).toMatch(/^Basic /);
  });

  it('keeps an omitted rotation and classifies only declared terminal errors', async () => {
    let status = 200;
    let body: Record<string, unknown> = { access_token: 'next', expires_in: 3600, token_type: 'Bearer', scope: 'files.read' };
    const fetcher: typeof fetch = async () => Response.json(body, { status });
    const driver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth, credentialFields, fetch: fetcher });
    expect((await driver.refresh(state(), new AbortController().signal)).credentials.refreshToken.reveal()).toBe('old-refresh');
    status = 400; body = { error: 'invalid_grant' };
    await expect(driver.refresh(state(), new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    body = { error: 'invalid_scope' };
    await expect(driver.refresh(state(), new AbortController().signal)).rejects.toThrow('HTTP 400');
  });

  it('never refreshes a method without a declared grant', async () => {
    const driver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth, credentialFields });
    expect(driver.refreshRequired({ ...state(), credentials: { ...credentials(), authMethodId: 'static' } })).toBe(false);
  });

  it('uses service-account key sources and declared credential copies', async () => {
    const { privateKey } = generateKeyPairSync('rsa', { modulusLength: 2048 });
    const delegatedAuth = {
      type: 'serviceAccount', fields: [
        { name: 'access_token', type: 'secretString' }, { name: 'service_account_key', type: 'secretString' },
        { name: 'delegated_user', type: 'string' }, { name: 'primary_email', type: 'string' },
      ], serviceAccount: { jwtBearer: {
        keyCredential: 'service_account_key', privateKey: { fromKey: 'private_key' },
        issuer: { fromKey: 'client_email' }, subject: { fromCredential: 'delegated_user' },
        audience: { fromKey: 'token_uri' }, tokenEndpoint: { fromKey: 'token_uri' },
        scopes: ['mail.read'], lifetime: '1h',
        credentialMappings: [{ credential: 'access_token', source: 'access_token' }],
        credentialCopies: [{ credential: 'primary_email', fromCredential: 'delegated_user' }],
      } },
    };
    const fields = { access_token: 'accessToken', service_account_key: 'serviceAccountKey', delegated_user: 'delegatedUser', primary_email: 'primaryEmail' };
    const key = JSON.stringify({ private_key: privateKey.export({ format: 'pem', type: 'pkcs8' }).toString(), client_email: 'service@example.test', token_uri: 'https://oauth.example.test/token' });
    const fetcher: typeof fetch = async (input, init) => {
      expect(String(input)).toBe('https://oauth.example.test/token');
      expect(new URLSearchParams(String(init?.body)).has('assertion')).toBe(true);
      return Response.json({ access_token: 'delegated', expires_in: 3600, token_type: 'Bearer', scope: 'mail.read' });
    };
    const driver = new ManifestCredentialRefreshDriver<{ accessToken: SecretString; serviceAccountKey: SecretString; delegatedUser: string; primaryEmail: string }>({ providerName: 'Example', auth: delegatedAuth, credentialFields: fields, fetch: fetcher });
    const result = await driver.refresh({ credentials: { accessToken: new SecretString(''), serviceAccountKey: new SecretString(key), delegatedUser: 'user@example.test', primaryEmail: '' }, now: new Date() }, new AbortController().signal);
    expect(result.credentials.accessToken.reveal()).toBe('delegated');
    expect(result.credentials.primaryEmail).toBe('user@example.test');
  });
});
