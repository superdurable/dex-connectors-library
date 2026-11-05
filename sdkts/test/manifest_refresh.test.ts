// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { generateKeyPairSync, verify } from 'node:crypto';
import { readFileSync } from 'node:fs';
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
  it('uses the default method and follows Go scope and missing-expiry rules', async () => {
    const configured = structuredClone(auth) as Record<string, unknown>;
    const method = (configured.methods as Record<string, unknown>[])[0]!;
    const refresh = ((method.oauth2 as Record<string, unknown>).refresh as Record<string, unknown>);
    refresh.requiredScopes = ['files.read'];
    let body: Record<string, unknown> = { access_token: 'next', expires_in: 3600, token_type: 'Bearer', scope: '\t  ' };
    let requests = 0;
    const driver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth: configured, credentialFields,
      fetch: async () => { requests++; return Response.json(body); } });
    const withoutMethod = { ...state(), credentials: { ...credentials(), authMethodId: '' } };
    await expect(driver.refresh(withoutMethod, new AbortController().signal)).resolves.toBeDefined();
    body = { ...body, scope: 'files.read\textra' };
    await expect(driver.refresh(withoutMethod, new AbortController().signal)).resolves.toBeDefined();
    body = { ...body, scope: 'files.read,extra' };
    await expect(driver.refresh(withoutMethod, new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    body = { access_token: 'next', token_type: 'Bearer', scope: 'files.read' };
    await expect(driver.refresh(withoutMethod, new AbortController().signal)).rejects.toThrow('omitted required fields');
    expect(requests).toBe(4);
  });

  it('refreshes schema-decoded provider declarations with declared token-type fields', async () => {
    const jiraAuth = JSON.parse(readFileSync(new URL('./fixtures/jira-auth.json', import.meta.url), 'utf8')) as Record<string, unknown>;
    const slackAuth = JSON.parse(readFileSync(new URL('./fixtures/slack-auth.json', import.meta.url), 'utf8')) as Record<string, unknown>;
    const jira = new ManifestCredentialRefreshDriver<{
      oauthClientId: string; oauthClientSecret: SecretString; accessToken: SecretString; refreshToken: SecretString;
    }>({ providerName: 'Jira', auth: jiraAuth, credentialFields: {
      oauth_client_id: 'oauthClientId', oauth_client_secret: 'oauthClientSecret',
      access_token: 'accessToken', refresh_token: 'refreshToken',
    }, fetch: async () => Response.json({ access_token: 'jira-new', refresh_token: 'jira-rotated', expires_in: 3600, scope: 'read:jira-work write:jira-work' }) });
    const jiraResult = await jira.refresh({ credentials: {
      oauthClientId: 'client', oauthClientSecret: new SecretString('secret'),
      accessToken: new SecretString('old'), refreshToken: new SecretString('jira-old'),
    }, now: new Date() }, new AbortController().signal);
    expect(jiraResult.credentials.refreshToken.reveal()).toBe('jira-rotated');

    const slackScopes = (slackAuth.oauth2 as Record<string, unknown>).scopes as string[];
    const slackUserScopes = (slackAuth.oauth2 as Record<string, unknown>).userScopes as string[];
    const slackRequests: string[] = [];
    const slack = new ManifestCredentialRefreshDriver<{
      oauthClientId: string; oauthClientSecret: SecretString; botToken: SecretString; botRefreshToken: SecretString;
      userToken: SecretString; userRefreshToken: SecretString;
    }>({ providerName: 'Slack', auth: slackAuth, credentialFields: {
      oauth_client_id: 'oauthClientId', oauth_client_secret: 'oauthClientSecret',
      bot_token: 'botToken', bot_refresh_token: 'botRefreshToken',
      user_token: 'userToken', user_refresh_token: 'userRefreshToken',
    }, fetch: async (_input, init) => {
      const refreshToken = new URLSearchParams(String(init?.body)).get('refresh_token') ?? '';
      slackRequests.push(refreshToken);
      return Response.json(refreshToken === 'bot-old'
        ? { access_token: 'bot-new', refresh_token: 'bot-rotated', expires_in: 3600, token_type: 'bot', scope: slackScopes.join(',') }
        : { access_token: 'user-new', refresh_token: 'user-rotated', expires_in: 3600, token_type: 'user', scope: slackUserScopes.join(',') });
    } });
    const slackResult = await slack.refresh({ credentials: {
      oauthClientId: 'client', oauthClientSecret: new SecretString('secret'),
      botToken: new SecretString('old'), botRefreshToken: new SecretString('bot-old'),
      userToken: new SecretString('old'), userRefreshToken: new SecretString('user-old'),
    }, now: new Date() }, new AbortController().signal);
    expect(slackRequests).toEqual(['bot-old', 'user-old']);
    expect(slackResult.credentials.userRefreshToken.reveal()).toBe('user-rotated');
  });

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

  it('keeps a non-expiring credential and respects declared missing token fields', async () => {
    const noExpiry = { credentials: { ...credentials(), refreshToken: new SecretString('') }, now: new Date() };
    const driver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth, credentialFields });
    expect(driver.refreshRequired(noExpiry)).toBe(false);

    const configured = structuredClone(auth) as Record<string, unknown>;
    const method = (configured.methods as Record<string, unknown>[])[0]!;
    const oauth = method.oauth2 as Record<string, unknown>;
    const refresh = oauth.refresh as Record<string, unknown>;
    refresh.acceptsMissingTokenType = true;
    refresh.missingExpiry = { lifetime: '2h' };
    const fetcher: typeof fetch = async () => Response.json({ access_token: 'next', scope: 'files.read' });
    const accepting = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth: configured, credentialFields, fetch: fetcher });
    const result = await accepting.refresh(state(), new AbortController().signal);
    expect(result.expiresAt.getTime()).toBeGreaterThan(Date.now() + 119 * 60_000);

    refresh.missingExpiry = 'keep';
    const keeping = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth: configured, credentialFields, fetch: fetcher });
    expect((await keeping.refresh(state(), new AbortController().signal)).expiresAt.getTime()).toBe(0);
  });

  it('refreshes stored credentials without expiry but rejects a response without expires_in', async () => {
    const configured = structuredClone(auth) as Record<string, unknown>;
    const method = (configured.methods as Record<string, unknown>[])[0]!;
    const refresh = ((method.oauth2 as Record<string, unknown>).refresh as Record<string, unknown>);
    refresh.storedMissingExpiry = 'refresh';
    refresh.acceptsMissingTokenType = true;
    let response: Record<string, unknown> = { access_token: 'next', expires_in: 3600, scope: 'files.read' };
    let requests = 0;
    const driver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth: configured,
      credentialFields, fetch: async () => { requests++; return Response.json(response); } });
    const noExpiry = { credentials: credentials(), now: new Date() };
    expect(driver.refreshRequired(noExpiry)).toBe(true);
    await expect(driver.refresh(noExpiry, new AbortController().signal)).resolves.toBeDefined();
    response = { access_token: 'next', scope: 'files.read' };
    await expect(driver.refresh(noExpiry, new AbortController().signal)).rejects.toThrow('omitted required fields');
    expect(requests).toBe(2);
  });

  it.each(['refresh', 'client_credentials', 'jwt_bearer'] as const)(
    'validates stored missing expiry separately from response lifetime for %s', grantKind => {
      const configured = grantKind === 'refresh' ? structuredClone(auth) as Record<string, unknown>
        : grantKind === 'client_credentials' ? { clientCredentials: {
          credentialMappings: [{ credential: 'access_token', source: 'access_token' }],
        } } as Record<string, unknown>
          : { serviceAccount: { jwtBearer: {
            credentialMappings: [{ credential: 'access_token', source: 'access_token' }],
          } } } as Record<string, unknown>;
      const grant = grantKind === 'refresh'
        ? (((configured.methods as Record<string, unknown>[])[0]!.oauth2 as Record<string, unknown>).refresh as Record<string, unknown>)
        : grantKind === 'client_credentials' ? configured.clientCredentials as Record<string, unknown>
          : (configured.serviceAccount as Record<string, unknown>).jwtBearer as Record<string, unknown>;
      const driver = () => new ManifestCredentialRefreshDriver<Credentials>({
        providerName: 'Example', auth: configured, credentialFields,
      });
      const undated = { credentials: credentials(), now: new Date() };
      grant.missingExpiry = { lifetime: '1h' };
      expect(driver().refreshRequired(undated)).toBe(false);
      grant.storedMissingExpiry = 'refresh';
      expect(driver().refreshRequired(undated)).toBe(true);
      grant.storedMissingExpiry = 'keep';
      expect(driver().refreshRequired(undated)).toBe(false);
      grant.storedMissingExpiry = 'invalid';
      expect(driver).toThrow('manifest storedMissingExpiry must be refresh or keep');
    });

  it('uses public authentication only when client secret is optional', async () => {
    const configured = structuredClone(auth) as Record<string, unknown>;
    const method = (configured.methods as Record<string, unknown>[])[0]!;
    const oauth = method.oauth2 as Record<string, unknown>;
    oauth.tokenEndpointAuthMethod = 'client_secret_post';
    oauth.tokenRequestEncoding = 'form';
    const fields = method.fields as Record<string, unknown>[];
    const clientSecret = fields.find(field => field.name === 'client_secret')!;
    clientSecret.required = true;
    let requests = 0;
    let requestBody = '';
    let authorization = '';
    const fetcher: typeof fetch = async (_input, init) => {
      requests++;
      requestBody = String(init?.body);
      authorization = String((init?.headers as Record<string, string>).Authorization ?? '');
      return Response.json({ access_token: 'next', expires_in: 3600, token_type: 'Bearer', scope: 'files.read' });
    };
    const withoutSecret = { ...credentials(), clientSecret: new SecretString('') };
    const required = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth: configured, credentialFields, fetch: fetcher });
    await expect(required.refresh({ credentials: withoutSecret, now: new Date() }, new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    expect(requests).toBe(0);

    delete clientSecret.required;
    (oauth.refresh as Record<string, unknown>).allowsPublicClient = true;
    const optional = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth: configured, credentialFields, fetch: fetcher });
    await optional.refresh({ credentials: withoutSecret, now: new Date() }, new AbortController().signal);
    expect(requests).toBe(1);
    expect(authorization).toBe('');
    expect(Object.fromEntries(new URLSearchParams(requestBody))).toEqual({
      grant_type: 'refresh_token', refresh_token: 'old-refresh', client_id: 'client-id', scope: 'offline_access files.read',
    });

    const confidential = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth: configured, credentialFields, fetch: fetcher });
    await confidential.refresh(state(), new AbortController().signal);
    expect(Object.fromEntries(new URLSearchParams(requestBody))).toEqual({
      grant_type: 'refresh_token', refresh_token: 'old-refresh', client_id: 'client-id', client_secret: 'client-secret', scope: 'offline_access files.read',
    });
  });

  it('treats an undeclared client secret as required before sending a token request', async () => {
    const configured = structuredClone(auth) as Record<string, unknown>;
    const method = (configured.methods as Record<string, unknown>[])[0]!;
    method.fields = (method.fields as Record<string, unknown>[]).filter(field => field.name !== 'client_secret');
    let requests = 0;
    const driver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth: configured, credentialFields,
      fetch: async () => { requests++; return Response.json({}); } });
    await expect(driver.refresh({ credentials: { ...credentials(), clientSecret: new SecretString('') }, now: new Date() },
      new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    expect(requests).toBe(0);
  });

  it('validates every grant mapping and Go duration before rotating a refresh token', async () => {
    const configured = structuredClone(auth) as Record<string, unknown>;
    const method = (configured.methods as Record<string, unknown>[])[0]!;
    const oauth = method.oauth2 as Record<string, unknown>;
    const refresh = oauth.refresh as Record<string, unknown>;
    refresh.missingExpiry = { lifetime: '1h2m3.5s4ms5us6ns' };
    let requests = 0;
    const driverFor = () => new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth: configured, credentialFields,
      fetch: async () => { requests++; return Response.json({ access_token: 'new', refresh_token: 'rotated', token_type: 'Bearer' }); } });
    const result = await driverFor().refresh(state(), new AbortController().signal);
    expect(result.credentials.refreshToken.reveal()).toBe('rotated');
    expect(result.expiresAt.getTime()).toBeGreaterThan(Date.now() + 3_723_000);
    refresh.missingExpiry = { lifetime: '.5h' };
    expect((await driverFor().refresh(state(), new AbortController().signal)).expiresAt.getTime()).toBeGreaterThan(Date.now() + 29 * 60_000);
    refresh.missingExpiry = { lifetime: '+1h' };
    expect((await driverFor().refresh(state(), new AbortController().signal)).expiresAt.getTime()).toBeGreaterThan(Date.now() + 59 * 60_000);
    refresh.missingExpiry = { lifetime: '0' };
    expect(driverFor).toThrow(TypeError);
    refresh.missingExpiry = { lifetime: '1fortnight' };
    expect(driverFor).toThrow(TypeError);
    refresh.missingExpiry = { lifetime: '2562048h' };
    expect(driverFor).toThrow(TypeError);
    refresh.missingExpiry = { lifetime: '-1h' };
    expect(driverFor).toThrow(TypeError);
    expect(requests).toBe(3);
    refresh.missingExpiry = 'keep';
    refresh.grants = [
      { refreshTokenCredential: 'refresh_token', credentialMappings: [{ credential: 'access_token', source: 'access_token' }] },
      { refreshTokenCredential: 'refresh_token', credentialMappings: [{ credential: 'missing_field', source: 'access_token' }] },
    ];
    await expect(driverFor().refresh(state(), new AbortController().signal)).rejects.toBeInstanceOf(TypeError);
    expect(requests).toBe(3);
  });

  it('uses the declared error code matrix for HTTP 400 and OAuth error responses', async () => {
    const codes = [
      'invalid_request', 'invalid_client', 'invalid_grant', 'unauthorized_client', 'unsupported_grant_type',
      'invalid_scope', 'access_denied', 'interaction_required', 'consent_required', 'temporarily_unavailable', 'server_error',
    ];
    let currentCode = '';
    const fetcher: typeof fetch = async () => Response.json({ error: currentCode, error_description: 'private detail' }, { status: 400 });
    const driver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth, credentialFields, fetch: fetcher });
    for (const code of codes) {
      currentCode = code;
      const rejected = driver.refresh(state(), new AbortController().signal);
      if (code === 'invalid_client' || code === 'invalid_grant') await expect(rejected).rejects.toBeInstanceOf(ReauthorizationRequiredError);
      else await expect(rejected).rejects.toThrow('HTTP 400');
    }
    const successStatusError: typeof fetch = async () => Response.json({ error: 'invalid_grant' });
    const accepting = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth, credentialFields, fetch: successStatusError });
    await expect(accepting.refresh(state(), new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
  });

  it('escapes endpoint templates like Go and preserves client-credential scopes', async () => {
    const tenant = "a/b c+d&e=f,g;h:i@j!k$l'm(n)o*p?q#r~s";
    const configured = {
      type: 'apiKey', fields: [
        { name: 'tenant_id', type: 'string' }, { name: 'client_id', type: 'string' },
        { name: 'client_secret', type: 'secretString' }, { name: 'access_token', type: 'secretString' },
      ], clientCredentials: {
        tokenEndpoint: { template: 'https://oauth.example.test/{tenant_id}/token' },
        clientIDCredential: 'client_id', clientSecretCredential: 'client_secret',
        scopes: ['offline_access', 'files.read'],
        credentialMappings: [{ credential: 'access_token', source: 'access_token' }],
      },
    };
    const fields = { tenant_id: 'tenantId', client_id: 'clientId', client_secret: 'clientSecret', access_token: 'accessToken' };
    const fetcher: typeof fetch = async (input, init) => {
      expect(String(input)).toBe('https://oauth.example.test/a%2Fb%20c+d&e=f%2Cg%3Bh:i@j%21k$l%27m%28n%29o%2Ap%3Fq%23r~s/token');
      expect(Object.fromEntries(new URLSearchParams(String(init?.body)))).toEqual({
        grant_type: 'client_credentials', client_id: 'client-id', client_secret: 'client-secret',
        scope: 'offline_access files.read',
      });
      return Response.json({ access_token: 'minted', expires_in: 3600, token_type: 'Bearer', scope: 'offline_access files.read' });
    };
    const driver = new ManifestCredentialRefreshDriver<{
      tenantId: string; clientId: string; clientSecret: SecretString; accessToken: SecretString;
    }>({ providerName: 'Example', auth: configured, credentialFields: fields, fetch: fetcher });
    const result = await driver.refresh({
      credentials: { tenantId: tenant, clientId: 'client-id', clientSecret: new SecretString('client-secret'), accessToken: new SecretString('') },
      now: new Date(),
    }, new AbortController().signal);
    expect(result.credentials.accessToken.reveal()).toBe('minted');
  });

  it('rotates both declared Slack-style grants and keeps the earliest expiry', async () => {
    const configured = {
      type: 'oauth2', fields: [
        { name: 'client_id', type: 'string', required: true },
        { name: 'client_secret', type: 'secretString', required: true },
        { name: 'bot_token', type: 'secretString' }, { name: 'bot_refresh_token', type: 'secretString' },
        { name: 'user_token', type: 'secretString' }, { name: 'user_refresh_token', type: 'secretString' },
      ], oauth2: {
        tokenEndpoint: 'https://slack.example.test/token', tokenEndpointAuthMethod: 'client_secret_basic',
        clientIDCredential: 'client_id', clientSecretCredential: 'client_secret',
        scopes: ['chat.write'], userScopes: ['history.read'],
        refresh: { scopeSeparators: 'whitespaceAndComma', grants: [
          { refreshTokenCredential: 'bot_refresh_token', acceptedTokenTypes: ['bot'], exactScopes: true, credentialMappings: [
            { credential: 'bot_token', source: 'access_token' }, { credential: 'bot_refresh_token', source: 'refresh_token' },
          ] },
          { refreshTokenCredential: 'user_refresh_token', acceptedTokenTypes: ['user'], exactScopes: true, grantedScopes: 'userScopes', credentialMappings: [
            { credential: 'user_token', source: 'access_token' }, { credential: 'user_refresh_token', source: 'refresh_token' },
          ] },
        ] },
      },
    };
    const fields = { client_id: 'clientId', client_secret: 'clientSecret', bot_token: 'botToken', bot_refresh_token: 'botRefreshToken', user_token: 'userToken', user_refresh_token: 'userRefreshToken' };
    const requests: string[] = [];
    const fetcher: typeof fetch = async (_input, init) => {
      const refreshToken = new URLSearchParams(String(init?.body)).get('refresh_token') ?? '';
      requests.push(refreshToken);
      return Response.json(refreshToken === 'bot-old'
        ? { access_token: 'bot-new', refresh_token: 'bot-rotated', expires_in: 3600, token_type: 'bot', scope: 'chat.write' }
        : { access_token: 'user-new', refresh_token: 'user-rotated', expires_in: 1800, token_type: 'user', scope: 'history.read' });
    };
    const driver = new ManifestCredentialRefreshDriver<{
      clientId: string; clientSecret: SecretString; botToken: SecretString; botRefreshToken: SecretString;
      userToken: SecretString; userRefreshToken: SecretString;
    }>({ providerName: 'Slack', auth: configured, credentialFields: fields, fetch: fetcher });
    const stored = {
      clientId: 'client-id', clientSecret: new SecretString('client-secret'),
      botToken: new SecretString('bot-access'), botRefreshToken: new SecretString('bot-old'),
      userToken: new SecretString('user-access'), userRefreshToken: new SecretString('user-old'),
    };
    expect(driver.refreshRequired({ credentials: stored, now: new Date() })).toBe(false);
    const result = await driver.refresh({ credentials: stored, expiresAt: new Date(0), now: new Date() }, new AbortController().signal);
    expect(requests).toEqual(['bot-old', 'user-old']);
    expect(result.credentials.botRefreshToken.reveal()).toBe('bot-rotated');
    expect(result.credentials.userRefreshToken.reveal()).toBe('user-rotated');
    expect(result.expiresAt.getTime()).toBeLessThan(Date.now() + 31 * 60_000);
    await expect(driver.refresh({ credentials: { ...stored, clientSecret: new SecretString('') }, now: new Date() },
      new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    expect(requests).toEqual(['bot-old', 'user-old']);
    const extraScopeDriver = new ManifestCredentialRefreshDriver({ providerName: 'Slack', auth: configured, credentialFields: fields,
      fetch: async () => Response.json({ access_token: 'bot-new', expires_in: 3600, token_type: 'bot', scope: 'chat.write extra' }) });
    await expect(extraScopeDriver.refresh({ credentials: stored, now: new Date() }, new AbortController().signal))
      .rejects.toBeInstanceOf(ReauthorizationRequiredError);
    const grants = (configured.oauth2.refresh.grants as Array<{ exactScopes: boolean }>);
    grants[0]!.exactScopes = false;
    const botOnly = { ...stored, userRefreshToken: new SecretString('') };
    await expect(extraScopeDriver.refresh({ credentials: botOnly, now: new Date() }, new AbortController().signal))
      .resolves.toMatchObject({ credentials: { botToken: new SecretString('bot-new') } });
  });

  it('uses declared separators for both all-scope and exact-scope checks', async () => {
    const configured = structuredClone(auth) as Record<string, unknown>;
    const oauth = ((configured.methods as Record<string, unknown>[])[0]!.oauth2 as Record<string, unknown>);
    const refresh = oauth.refresh as Record<string, unknown>;
    refresh.requiredScopes = ['files.read', 'files.write'];
    let returnedScope = '';
    const driver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth: configured, credentialFields,
      fetch: async () => Response.json({ access_token: 'next', expires_in: 3600, token_type: 'Bearer', scope: returnedScope }) });
    const check = async (scope: string, accepted: boolean) => {
      returnedScope = scope;
      const result = driver.refresh(state(), new AbortController().signal);
      if (accepted) await expect(result).resolves.toBeDefined();
      else await expect(result).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    };
    await check('files.read\tfiles.write', true);
    await check('files.read,files.write', false);
    refresh.scopeSeparators = 'whitespaceAndComma';
    await check('files.read,files.write', true);
    await check('files.read\tfiles.write', false);
    refresh.exactScopes = true;
    await check('files.read,files.write', true);
    await check('files.read,files.write,extra', false);
    await check('files.read\tfiles.write', false);
    refresh.scopeSeparators = 'whitespace';
    await check('files.read\tfiles.write', true);
    await check('files.read,files.write', false);
  });

  it('accepts any declared alternative scope set and rejects unrelated scopes', async () => {
    const configured = structuredClone(auth) as Record<string, unknown>;
    const oauth = ((configured.methods as Record<string, unknown>[])[0]!.oauth2 as Record<string, unknown>);
    const refresh = oauth.refresh as Record<string, unknown>;
    refresh.acceptedScopeSets = [['files.read', 'offline_access'], ['files.manage']];
    let returnedScope = '';
    const driver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth: configured, credentialFields,
      fetch: async () => Response.json({ access_token: 'next', expires_in: 3600, token_type: 'Bearer', scope: returnedScope }) });
    for (const scope of ['', 'files.read', 'files.manage', 'files.manage extra']) {
      returnedScope = scope;
      await expect(driver.refresh(state(), new AbortController().signal)).resolves.toBeDefined();
    }
    returnedScope = 'files.other';
    await expect(driver.refresh(state(), new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    refresh.exactScopes = true;
    returnedScope = 'files.manage extra';
    await expect(driver.refresh(state(), new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
  });

  it('ignores reported scope only when the manifest declares the Go driver policy', async () => {
    const configured = structuredClone(auth) as Record<string, unknown>;
    const oauth = (configured.methods as Record<string, unknown>[])[0]!.oauth2 as Record<string, unknown>;
    const refresh = oauth.refresh as Record<string, unknown>;
    const makeDriver = () => new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth: configured,
      credentialFields, fetch: async () => Response.json({ access_token: 'next', expires_in: 3600, token_type: 'Bearer', scope: 'other' }) });
    await expect(makeDriver().refresh(state(), new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    refresh.responseScopes = 'ignore';
    await expect(makeDriver().refresh(state(), new AbortController().signal)).resolves.toBeDefined();
  });

  it('never refreshes a method without a declared grant', async () => {
    const driver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth, credentialFields });
    expect(driver.refreshRequired({ ...state(), credentials: { ...credentials(), authMethodId: 'static' } })).toBe(false);
    let requests = 0;
    const forbidden: typeof fetch = async () => { requests++; return Response.json({}); };
    const staticDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Example', auth, credentialFields, fetch: forbidden });
    await expect(staticDriver.refresh({ ...state(), credentials: { ...credentials(), authMethodId: 'static' } }, new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    expect(requests).toBe(0);
  });

  it('uses service-account key sources and declared credential copies', async () => {
    const { privateKey, publicKey } = generateKeyPairSync('rsa', { modulusLength: 2048 });
    const delegatedAuth = {
      type: 'serviceAccount', fields: [
        { name: 'access_token', type: 'secretString' }, { name: 'service_account_key', type: 'secretString' },
        { name: 'delegated_user', type: 'string' }, { name: 'primary_email', type: 'string' },
      ], serviceAccount: { jwtBearer: {
        keyCredential: 'service_account_key', privateKey: { fromKey: 'private_key' },
        issuer: { fromKey: 'client_email' }, subject: { fromCredential: 'delegated_user' },
        subjectValidation: 'goMailAddress',
        audience: { fromKey: 'token_uri' }, tokenEndpoint: { fromKey: 'token_uri' },
        scopes: ['offline_access', 'mail.read'], lifetime: '1h',
        acceptedScopeSets: [['mail.read'], ['mail.manage']], scopeSeparators: 'whitespaceAndComma',
        credentialMappings: [{ credential: 'access_token', source: 'access_token' }],
        credentialCopies: [{ credential: 'primary_email', fromCredential: 'delegated_user' }],
      } },
    };
    const fields = { access_token: 'accessToken', service_account_key: 'serviceAccountKey', delegated_user: 'delegatedUser', primary_email: 'primaryEmail' };
    const key = JSON.stringify({ private_key: privateKey.export({ format: 'pem', type: 'pkcs8' }).toString(), client_email: 'service@example.test', token_uri: 'https://oauth.example.test/token' });
    let returnedScope = 'offline_access mail.read';
    const fetcher: typeof fetch = async (input, init) => {
      expect(String(input)).toBe('https://oauth.example.test/token');
      const assertion = new URLSearchParams(String(init?.body)).get('assertion') ?? '';
      const segments = assertion.split('.');
      expect(segments).toHaveLength(3);
      const claims = JSON.parse(Buffer.from(segments[1]!, 'base64url').toString()) as Record<string, unknown>;
      expect(claims).toMatchObject({ iss: 'service@example.test', sub: 'user@example.test', aud: 'https://oauth.example.test/token', scope: 'offline_access mail.read' });
      expect(Number(claims.exp) - Number(claims.iat)).toBe(3600);
      expect(verify('RSA-SHA256', Buffer.from(`${segments[0]}.${segments[1]}`), publicKey, Buffer.from(segments[2]!, 'base64url'))).toBe(true);
      return Response.json({ access_token: 'delegated', expires_in: 3600, token_type: 'Bearer', scope: returnedScope });
    };
    const driver = new ManifestCredentialRefreshDriver<{ accessToken: SecretString; serviceAccountKey: SecretString; delegatedUser: string; primaryEmail: string }>({ providerName: 'Example', auth: delegatedAuth, credentialFields: fields, fetch: fetcher });
    const result = await driver.refresh({ credentials: { accessToken: new SecretString(''), serviceAccountKey: new SecretString(key), delegatedUser: 'user@example.test', primaryEmail: '' }, now: new Date() }, new AbortController().signal);
    expect(result.credentials.accessToken.reveal()).toBe('delegated');
    expect(result.credentials.primaryEmail).toBe('user@example.test');
    returnedScope = 'mail.manage,extra';
    await expect(driver.refresh({ credentials: { accessToken: new SecretString(''), serviceAccountKey: new SecretString(key), delegatedUser: 'user@example.test', primaryEmail: '' }, now: new Date() },
      new AbortController().signal)).resolves.toBeDefined();
    returnedScope = 'mail.other';
    await expect(driver.refresh({ credentials: { accessToken: new SecretString(''), serviceAccountKey: new SecretString(key), delegatedUser: 'user@example.test', primaryEmail: '' }, now: new Date() },
      new AbortController().signal)).rejects.toBeInstanceOf(ReauthorizationRequiredError);
    const retryable = new ManifestCredentialRefreshDriver({ providerName: 'Example', auth: delegatedAuth, credentialFields: fields,
      fetch: async () => { throw new Error('temporary network failure'); } });
    await expect(retryable.refresh({ credentials: { accessToken: new SecretString(''), serviceAccountKey: new SecretString(key), delegatedUser: 'user@example.test', primaryEmail: '' }, now: new Date() },
      new AbortController().signal)).rejects.toThrow('Example token endpoint is unavailable');
    const invalidLifetime = structuredClone(delegatedAuth);
    invalidLifetime.serviceAccount.jwtBearer.lifetime = 'invalid';
    const invalid = new ManifestCredentialRefreshDriver({ providerName: 'Example', auth: invalidLifetime, credentialFields: fields,
      fetch: async () => { throw new Error('should not send'); } });
    await expect(invalid.refresh({ credentials: { accessToken: new SecretString(''), serviceAccountKey: new SecretString(key), delegatedUser: 'user@example.test', primaryEmail: '' }, now: new Date() },
      new AbortController().signal)).rejects.toBeInstanceOf(TypeError);
    invalidLifetime.serviceAccount.jwtBearer.lifetime = '500us';
    await expect(invalid.refresh({ credentials: { accessToken: new SecretString(''), serviceAccountKey: new SecretString(key), delegatedUser: 'user@example.test', primaryEmail: '' }, now: new Date() },
      new AbortController().signal)).rejects.toThrow('at least one millisecond');
  });

  it('matches Go mail.ParseAddress for delegated subjects before any token request', async () => {
    const { privateKey } = generateKeyPairSync('rsa', { modulusLength: 2048 });
    const key = JSON.stringify({ private_key: privateKey.export({ format: 'pem', type: 'pkcs8' }).toString(),
      client_email: 'service@example.test', token_uri: 'https://oauth.example.test/token' });
    const delegatedAuth = { type: 'serviceAccount', serviceAccount: { jwtBearer: {
      keyCredential: 'service_account_key', privateKey: { fromKey: 'private_key' }, issuer: { fromKey: 'client_email' },
      subject: { fromCredential: 'delegated_user' }, subjectValidation: 'goMailAddress',
      audience: { fromKey: 'token_uri' }, tokenEndpoint: { fromKey: 'token_uri' }, scopes: ['mail.read'], lifetime: '1h',
      credentialMappings: [{ credential: 'access_token', source: 'access_token' }],
    } } };
    let requests = 0;
    const driver = new ManifestCredentialRefreshDriver({ providerName: 'Google', auth: delegatedAuth,
      credentialFields: { access_token: 'accessToken', service_account_key: 'serviceAccountKey', delegated_user: 'delegatedUser' },
      fetch: async () => { requests++; return Response.json({ access_token: 'next', expires_in: 3600, token_type: 'Bearer' }); } });
    const cases = readFileSync(new URL('./fixtures/go-mail-addresses.jsonl', import.meta.url), 'utf8').trim().split('\n')
      .map(line => JSON.parse(line) as { input: string; accepted: boolean });
    for (const testCase of cases) {
      const previousRequests = requests;
      const refresh = driver.refresh({ credentials: { accessToken: new SecretString(''), serviceAccountKey: new SecretString(key),
        delegatedUser: testCase.input }, now: new Date() }, new AbortController().signal);
      if (testCase.accepted) {
        await expect(refresh, testCase.input).resolves.toBeDefined();
        expect(requests).toBe(previousRequests + 1);
      } else {
        await expect(refresh, testCase.input).rejects.toBeInstanceOf(ReauthorizationRequiredError);
        expect(requests).toBe(previousRequests);
      }
    }
  });

  it('retries a Help Scout style response without positive expires_in', async () => {
    const clientAuth = { fields: [{ name: 'access_token', type: 'secretString' }], clientCredentials: {
      tokenEndpoint: 'https://oauth.example.test/token', clientIDCredential: 'client_id',
      clientSecretCredential: 'client_secret', missingExpiry: 'reject',
      credentialMappings: [{ credential: 'access_token', source: 'access_token' }],
    } };
    let response: Record<string, unknown> = { access_token: 'next', token_type: 'Bearer' };
    const driver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Help Scout', auth: clientAuth,
      credentialFields, fetch: async () => Response.json(response) });
    for (const expiry of [undefined, 0, -1]) {
      response = { access_token: 'next', token_type: 'Bearer', ...(expiry === undefined ? {} : { expires_in: expiry }) };
      await expect(driver.refresh(state(), new AbortController().signal)).rejects.toThrow('omitted required fields');
    }
    response = { access_token: 'next', token_type: 'Bearer', expires_in: 3600 };
    await expect(driver.refresh(state(), new AbortController().signal)).resolves.toBeDefined();
  });

  it('rejects ignored client scope sets and invalid JWT declarations at construction', () => {
    const clientAuth = { clientCredentials: { acceptedScopeSets: [['read']], tokenEndpoint: 'https://oauth.example.test/token',
      credentialMappings: [{ credential: 'access_token', source: 'access_token' }] } };
    expect(() => new ManifestCredentialRefreshDriver({ providerName: 'Example', auth: clientAuth, credentialFields })).toThrow(TypeError);
    const jwtAuth = { serviceAccount: { jwtBearer: { lifetime: '1h',
      credentialMappings: [{ credential: 'access_token', source: 'access_token' }] } } };
    const assertion = jwtAuth.serviceAccount.jwtBearer as Record<string, unknown>;
    for (const declaration of [{ subjectValidation: 'other' }, { subjectValidation: null },
      { subjectValidation: 'goMailAddress' }, { scopeSeparators: null }, { scopeSeparators: 'other' },
      { acceptedScopeSets: null }]) {
      Object.assign(assertion, declaration);
      expect(() => new ManifestCredentialRefreshDriver({ providerName: 'Example', auth: jwtAuth, credentialFields })).toThrow(TypeError);
      for (const key of Object.keys(declaration)) delete assertion[key];
    }
  });

  it('rejects unknown manifest response declarations', () => {
    for (const declaration of [{ missingExpiry: 'unknown' }, { acceptedScopeSets: [] }, { responseScopes: 'unknown' },
      { acceptedScopeSets: [['read'], []] }, { scopeSeparators: 'comma' },
      { missingExpiry: null }, { acceptedScopeSets: null }, { scopeSeparators: null },
      { missingExpiry: { lifetime: '0s' } }, { missingExpiry: { lifetime: 'nonsense' } }]) {
      const configured = structuredClone(auth) as Record<string, unknown>;
      const method = (configured.methods as Record<string, unknown>[])[0]!;
      Object.assign((method.oauth2 as Record<string, unknown>).refresh as object, declaration);
      expect(() => new ManifestCredentialRefreshDriver({ providerName: 'Example', auth: configured, credentialFields })).toThrow(TypeError);
    }
  });
});
