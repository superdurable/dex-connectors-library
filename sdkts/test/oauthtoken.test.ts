// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { generateKeyPairSync, verify } from 'node:crypto';
import { describe, expect, it } from 'vitest';
import { SecretString } from '../src/credentials.js';
import { ReauthorizationRequiredError } from '../src/errors.js';
import { TokenEndpoint, forcedRefreshFloorMs, hasAllScopes, hasExactScopes, isRefreshRequired, isRefreshRequiredWithIssueTime, parseRSAPrivateKeyPEM, proportionalRefreshSkewMs, signJWTBearerAssertion, type ClientCredentials } from '../src/oauthtoken/index.js';

const client: ClientCredentials = { id: 'app-id', secret: new SecretString('client-secret'), authenticationMethod: 'client_secret_post' };
const success = { access_token: 'new-access', refresh_token: 'rotated-refresh', expires_in: 3600, token_type: 'Bearer', scope: 'mail.read mail.send' };
function endpoint(fetcher: typeof fetch, overrides: Partial<ConstructorParameters<typeof TokenEndpoint>[0]> = {}) {
  return new TokenEndpoint({ providerName: 'Example', url: 'https://auth.example.test/token', fetch: fetcher, terminalErrorCodes: ['invalid_grant', 'invalid_client'], ...overrides });
}

describe('OAuth token exchange', () => {
  it('exchanges a refresh token and preserves rotated material', async () => {
    let requestBody = '';
    const fetcher: typeof fetch = async (_input, init) => {
      requestBody = String(init?.body);
      expect(init?.redirect).toBe('manual');
      return Response.json(success);
    };
    const result = await endpoint(fetcher).exchangeRefreshToken(client, new SecretString('old-refresh'));
    expect(new URLSearchParams(requestBody).get('refresh_token')).toBe('old-refresh');
    expect(new URLSearchParams(requestBody).get('client_secret')).toBe('client-secret');
    expect(result.accessToken.reveal()).toBe('new-access');
    expect(result.nextRefreshToken(new SecretString('old-refresh')).reveal()).toBe('rotated-refresh');
    expect(result.expiresInMs).toBe(3_600_000);
    expect(() => JSON.stringify(result)).toThrow('cannot be serialized');
  });

  it('supports Basic and public clients, JSON grants, and declared fields', async () => {
    const requests: RequestInit[] = [];
    const fetcher: typeof fetch = async (_input, init) => { requests.push(init ?? {}); return Response.json({ ...success, refresh_token: '', instance_url: 'https://instance.example.test', signature: 'private' }); };
    const basic = { ...client, authenticationMethod: 'client_secret_basic' } as const;
    const result = await endpoint(fetcher, { usesJSONRequestBody: true, retainedResponseFields: ['instance_url'] }).exchangeRefreshToken(basic, new SecretString('old'));
    expect(JSON.parse(String(requests[0]?.body))).toMatchObject({ grant_type: 'refresh_token', refresh_token: 'old' });
    expect(String((requests[0]?.headers as Record<string, string>).Authorization)).toMatch(/^Basic /);
    expect(result.nextRefreshToken(new SecretString('old')).reveal()).toBe('old');
    expect(result.retainedFields).toEqual({ instance_url: 'https://instance.example.test' });
    const publicClient = { id: 'public-id', authenticationMethod: 'none' } as const;
    await endpoint(fetcher).exchangeRefreshToken(publicClient, new SecretString('old'));
    expect(new URLSearchParams(String(requests[1]?.body)).get('client_id')).toBe('public-id');
    expect((requests[1]?.headers as Record<string, string>).Authorization).toBeUndefined();
  });

  it('exchanges client credentials and a JWT assertion without leaking material', async () => {
    const requests: RequestInit[] = [];
    const fetcher: typeof fetch = async (_input, init) => { requests.push(init ?? {}); return Response.json(success); };
    await endpoint(fetcher).exchangeClientCredentials(client, ['mail.read', 'mail.send']);
    expect(new URLSearchParams(String(requests[0]?.body)).get('scope')).toBe('mail.read mail.send');
    await endpoint(fetcher).exchangeJWTBearerAssertion(new SecretString('signed.assertion'));
    expect([...new URLSearchParams(String(requests[1]?.body)).keys()].sort()).toEqual(['assertion', 'grant_type']);
    await expect(endpoint(fetcher).exchangeClientCredentials({ ...client, authenticationMethod: 'none' })).rejects.toThrow('confidential client');
    await expect(endpoint(fetcher).exchangeRefreshToken(client, new SecretString(''))).rejects.toBeInstanceOf(ReauthorizationRequiredError);
  });

  it('classifies only declared sub-500 terminal errors as reauthorization', async () => {
    const terminal: typeof fetch = async () => Response.json({ error: 'invalid_grant', error_description: 'secret-provider-detail' }, { status: 403 });
    await expect(endpoint(terminal).exchangeRefreshToken(client, new SecretString('refresh-secret'))).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    const outage: typeof fetch = async () => Response.json({ error: 'invalid_grant' }, { status: 503 });
    await expect(endpoint(outage).exchangeRefreshToken(client, new SecretString('refresh-secret'))).rejects.toThrow('HTTP 503');
    const unavailable: typeof fetch = async () => { throw new Error('refresh-secret'); };
    await expect(endpoint(unavailable).exchangeRefreshToken(client, new SecretString('refresh-secret'))).rejects.toThrow('endpoint is unavailable');
  });

  it('rejects redirects, oversized bodies, invalid responses, and unsafe endpoint setup', async () => {
    const redirect: typeof fetch = async () => Response.json({}, { status: 307, headers: { Location: 'https://attacker.test/' } });
    await expect(endpoint(redirect).exchangeRefreshToken(client, new SecretString('old'))).rejects.toThrow('HTTP 307');
    const oversized: typeof fetch = async () => Response.json({ access_token: 'a'.repeat(1 << 20) });
    await expect(endpoint(oversized).exchangeRefreshToken(client, new SecretString('old'))).rejects.toThrow('could not be read');
    const incomplete: typeof fetch = async () => Response.json({ access_token: 'a', expires_in: 0, token_type: 'Bearer' });
    await expect(endpoint(incomplete).exchangeRefreshToken(client, new SecretString('old'))).rejects.toThrow('omitted required fields');
    await expect(endpoint(incomplete, { url: 'http://auth.example.test/token' }).exchangeRefreshToken(client, new SecretString('old'))).rejects.toThrow('endpoint is invalid');
    await expect(endpoint(incomplete, { url: 'https://user:password@auth.example.test/token' }).exchangeRefreshToken(client, new SecretString('old'))).rejects.toThrow('endpoint is invalid');
    await expect(endpoint(incomplete, { retainedResponseFields: ['access_token'] }).exchangeRefreshToken(client, new SecretString('old'))).rejects.toThrow('cannot retain');
    await expect(endpoint(incomplete, { additionalRequestParameters: { grant_type: 'other' } }).exchangeRefreshToken(client, new SecretString('old'))).rejects.toThrow('cannot replace');
  });

  it('accepts declared missing fields and plain HTTP', async () => {
    const missing: typeof fetch = async () => Response.json({ access_token: 'a' });
    const result = await endpoint(missing, { url: 'http://identity.internal/token', allowsPlainHTTP: true, acceptsMissingExpiresIn: true, acceptsMissingTokenType: true }).exchangeRefreshToken(client, new SecretString('old'));
    expect(result.expiresInMs).toBe(0);
    expect(result.tokenType).toBe('');
  });
});

describe('OAuth refresh rules and JWT assertion', () => {
  it('uses proportional skew and missing-expiry policy', () => {
    const issuedAt = new Date('2026-01-01T00:00:00Z');
    const shortExpiry = new Date(issuedAt.getTime() + 60_000);
    expect(proportionalRefreshSkewMs(issuedAt, shortExpiry)).toBe(15_000);
    expect(forcedRefreshFloorMs(issuedAt, shortExpiry)).toBe(15_000);
    expect(isRefreshRequired(true, shortExpiry, new Date(shortExpiry.getTime() - 10_000), 'refresh', issuedAt)).toBe(true);
    expect(isRefreshRequiredWithIssueTime(true, issuedAt, shortExpiry, new Date(shortExpiry.getTime() - 20_000), 'refresh')).toBe(false);
    expect(isRefreshRequired(true, undefined, issuedAt, 'keep')).toBe(false);
    expect(isRefreshRequired(false, undefined, issuedAt, 'keep')).toBe(true);
  });

  it('compares returned scopes as sets', () => {
    expect(hasAllScopes('mail.read,mail.send extra', ['mail.read', 'mail.send'])).toBe(true);
    expect(hasExactScopes('mail.send,mail.read', ['mail.read', 'mail.send'])).toBe(true);
    expect(hasExactScopes('mail.read extra', ['mail.read'])).toBe(false);
  });

  it('signs a valid RS256 JWT and sanitizes key errors', () => {
    const { privateKey, publicKey } = generateKeyPairSync('rsa', { modulusLength: 2048 });
    const parsed = parseRSAPrivateKeyPEM(privateKey.export({ type: 'pkcs8', format: 'pem' }).toString());
    const signed = signJWTBearerAssertion({ issuer: 'service@example.test', subject: 'user@example.test', audience: 'https://auth.example.test/token', scope: 'mail.read', issuedAt: new Date('2026-01-01T00:00:00Z'), lifetimeMs: 3_600_000 }, parsed).reveal();
    const segments = signed.split('.');
    expect(segments).toHaveLength(3);
    expect(JSON.parse(Buffer.from(segments[1]!, 'base64url').toString())).toMatchObject({ iss: 'service@example.test', sub: 'user@example.test', scope: 'mail.read' });
    expect(verify('RSA-SHA256', Buffer.from(`${segments[0]}.${segments[1]}`), publicKey, Buffer.from(segments[2]!, 'base64url'))).toBe(true);
    expect(() => parseRSAPrivateKeyPEM('private-secret')).toThrow('private key is invalid');
  });
});
