// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { generateKeyPairSync } from 'node:crypto';
import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString } from '@superdurable/connectors-sdk';
import { GmailCredentialRefreshDriver } from '../src/refresh.js';
import type { Credentials } from '../src/models.js';

const now = new Date('2026-01-02T00:00:00Z');
const oauth: Credentials = {
  authMethodId: 'google-oauth', oauthClientId: 'client.apps.googleusercontent.com',
  oauthClientSecret: new SecretString('client-secret'), refreshToken: new SecretString('old-refresh-token'),
  accessToken: new SecretString('old-access-token'), primaryEmail: 'owner@example.com',
};

describe('Gmail credential refresh driver', () => {
  it('refreshes absent or soon-expiring access tokens', () => {
    const driver = new GmailCredentialRefreshDriver();
    expect(driver.refreshRequired({ credentials: oauth, now })).toBe(true);
    expect(driver.refreshRequired({ credentials: oauth, now, expiresAt: new Date(now.getTime() + 60_000) })).toBe(true);
    expect(driver.refreshRequired({ credentials: oauth, now, expiresAt: new Date(now.getTime() + 600_000) })).toBe(false);
  });

  it('exchanges Google OAuth refresh tokens and retains an unrotated token', async () => {
    const driver = new GmailCredentialRefreshDriver(async (url, options) => {
      expect(url).toBe('https://oauth2.googleapis.com/token');
      const parameters = new URLSearchParams(String(options?.body));
      expect(parameters.get('grant_type')).toBe('refresh_token');
      expect(parameters.get('client_id')).toBe('client.apps.googleusercontent.com');
      expect(parameters.get('client_secret')).toBe('client-secret');
      expect(parameters.get('refresh_token')).toBe('old-refresh-token');
      return Response.json({ access_token: 'new-access-token', expires_in: 3600, token_type: 'Bearer', scope: 'https://www.googleapis.com/auth/gmail.readonly https://www.googleapis.com/auth/gmail.send' });
    }, () => now);
    const result = await driver.refresh({ credentials: oauth, now }, new AbortController().signal);
    expect(result.credentials.accessToken.reveal()).toBe('new-access-token');
    expect(result.credentials.refreshToken?.reveal()).toBe('old-refresh-token');
    expect(result.expiresAt.toISOString()).toBe('2026-01-02T01:00:00.000Z');
    const defaultMethodResult = await driver.refresh({ credentials: { ...oauth, authMethodId: '' }, now }, new AbortController().signal);
    expect(defaultMethodResult.credentials.accessToken.reveal()).toBe('new-access-token');
  });

  it('stores a rotated token and rejects missing scopes or invalid grants', async () => {
    const rotated = new GmailCredentialRefreshDriver(async () => Response.json({ access_token: 'new-access-token', refresh_token: 'next-refresh-token', expires_in: 3600, token_type: 'Bearer' }), () => now);
    expect((await rotated.refresh({ credentials: oauth, now }, new AbortController().signal)).credentials.refreshToken?.reveal()).toBe('next-refresh-token');
    const missingScope = new GmailCredentialRefreshDriver(async () => Response.json({ access_token: 'new-access-token', expires_in: 3600, token_type: 'Bearer', scope: 'https://www.googleapis.com/auth/gmail.readonly' }), () => now);
    await expect(missingScope.refresh({ credentials: oauth, now }, new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    const revoked = new GmailCredentialRefreshDriver(async () => Response.json({ error: 'invalid_grant' }, { status: 400 }), () => now);
    await expect(revoked.refresh({ credentials: oauth, now }, new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    const missingExpiry = new GmailCredentialRefreshDriver(async () => Response.json({ access_token: 'new-access-token', token_type: 'Bearer' }), () => now);
    await expect(missingExpiry.refresh({ credentials: oauth, now }, new AbortController().signal)).rejects.toThrow('omitted required fields');
  });

  it('signs a delegated Workspace assertion and copies the delegated mailbox', async () => {
    const { privateKey } = generateKeyPairSync('rsa', { modulusLength: 2048 });
    const workspace: Credentials = {
      authMethodId: 'workspace-domain-delegation', accessToken: new SecretString(''), primaryEmail: '', delegatedUser: '  staff@example.com  ',
      serviceAccountKey: new SecretString(JSON.stringify({ client_email: 'service@example.iam.gserviceaccount.com', private_key: privateKey.export({ format: 'pem', type: 'pkcs8' }).toString(), token_uri: 'https://token.example.test/token' })),
    };
    const driver = new GmailCredentialRefreshDriver(async (url, options) => {
      expect(url).toBe('https://token.example.test/token');
      const parameters = new URLSearchParams(String(options?.body));
      expect(parameters.get('grant_type')).toBe('urn:ietf:params:oauth:grant-type:jwt-bearer');
      const assertion = parameters.get('assertion') ?? '';
      const claims = JSON.parse(Buffer.from(assertion.split('.')[1] ?? '', 'base64url').toString()) as Record<string, string>;
      expect(claims.sub).toBe('staff@example.com');
      expect(claims.scope).toContain('gmail.send');
      return Response.json({ access_token: 'delegated-access-token', expires_in: 3600, token_type: 'Bearer' });
    }, () => now);
    const result = await driver.refresh({ credentials: workspace, now }, new AbortController().signal);
    expect(result.credentials.primaryEmail).toBe('staff@example.com');
    expect(result.credentials.delegatedUser).toBe('  staff@example.com  ');
    expect(result.credentials.accessToken.reveal()).toBe('delegated-access-token');
  });

  it('rejects invalid delegation before exchange and leaves unknown methods retryable', async () => {
    const validKey = { client_email: 'service@example.com', private_key: 'key', token_uri: 'https://example.com/token' };
    const workspace: Credentials = { ...oauth, authMethodId: 'workspace-domain-delegation', delegatedUser: 'user@example.com',
      serviceAccountKey: new SecretString(JSON.stringify({ ...validKey, token_uri: 'http://example.com/token' })) };
    const driver = new GmailCredentialRefreshDriver(async () => { throw new Error('unexpected token request'); });
    await expect(driver.refresh({ credentials: workspace, now }, new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    await expect(driver.refresh({ credentials: { ...workspace, delegatedUser: 'invalid', serviceAccountKey: new SecretString(JSON.stringify(validKey)) }, now },
      new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    await expect(driver.refresh({ credentials: { ...workspace, authMethodId: 'other' }, now }, new AbortController().signal))
      .rejects.toThrow('not supported');
  });
});
