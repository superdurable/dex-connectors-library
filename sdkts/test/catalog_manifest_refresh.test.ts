// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { generateKeyPairSync } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { parse } from 'yaml';
import { ManifestCredentialRefreshDriver } from '../src/oauthtoken/manifest_refresh.js';
import { SecretString } from '../src/credentials.js';

type ManifestObject = Record<string, unknown>;

function object(value: unknown): ManifestObject {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as ManifestObject : {};
}

function list(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function hasTokenGrant(auth: ManifestObject): boolean {
  const methods = list(auth.methods).length ? list(auth.methods).map(object) : [auth];
  return methods.some(method => {
    if (method.clientCredentials || object(method.serviceAccount).jwtBearer) return true;
    const oauth = object(method.oauth2);
    const refresh = object(oauth.refresh);
    return list(refresh.grants).length > 0 || list(oauth.credentialMappings)
      .some(mapping => object(mapping).source === 'refresh_token');
  });
}

const catalog = object(parse(readFileSync(new URL('../../catalog.yaml', import.meta.url), 'utf8')));
const manifests = list(catalog.directories).map(directory => {
  const path = String(directory);
  const manifest = object(parse(readFileSync(new URL(`../../${path}/connector.yaml`, import.meta.url), 'utf8')));
  return { path, auth: object(object(manifest.spec).auth) };
}).filter(item => hasTokenGrant(item.auth));

describe('catalog manifest refresh declarations', () => {
  it('includes every generated Go token-grant connector', () => {
    expect(manifests.length).toBeGreaterThanOrEqual(35);
    expect(manifests.some(item => item.path === 'connectors/salesforce')).toBe(true);
  });

  it.each(manifests)('$path constructs every grant', ({ path, auth }) => {
    const fields = [
      ...list(auth.fields),
      ...list(auth.methods).flatMap(method => list(object(method).fields)),
    ].map(object);
    const credentialFields = Object.fromEntries(fields.map(field => [String(field.name), String(field.name)]));
    const driver = new ManifestCredentialRefreshDriver<Record<string, unknown>>({
      providerName: path, auth, credentialFields,
    });
    expect(driver).toBeDefined();
  });

  it('mints Salesforce JWT bearer tokens from a credential-backed private key', async () => {
    const auth = manifests.find(item => item.path === 'connectors/salesforce')!.auth;
    const { privateKey } = generateKeyPairSync('rsa', { modulusLength: 2048 });
    const privateKeyPEM = privateKey.export({ format: 'pem', type: 'pkcs8' }).toString();
    const requests: string[] = [];
    const driver = new ManifestCredentialRefreshDriver<Record<string, unknown>>({
      providerName: 'Salesforce', auth,
      credentialFields: Object.fromEntries([
        'oauth_client_id', 'jwt_username', 'jwt_private_key', 'jwt_login_environment', 'access_token', 'instance_url',
      ].map(name => [name, name])),
      fetch: async (input, init) => {
        requests.push(String(input));
        const assertion = new URLSearchParams(String(init?.body)).get('assertion') ?? '';
        const claims = JSON.parse(Buffer.from(assertion.split('.')[1] ?? '', 'base64url').toString()) as ManifestObject;
        expect(claims).toMatchObject({
          iss: 'consumer-key', sub: 'integration@example.com', aud: 'https://test.salesforce.com',
        });
        return Response.json({
          access_token: 'minted', instance_url: 'https://example.my.salesforce.com',
          expires_in: 3600, token_type: 'Bearer', scope: 'api',
        });
      },
    });
    const result = await driver.refresh({ credentials: {
      authMethodId: 'salesforce-jwt-bearer', oauth_client_id: 'consumer-key',
      jwt_username: 'integration@example.com', jwt_private_key: new SecretString(privateKeyPEM),
      jwt_login_environment: 'sandbox', access_token: new SecretString(''), instance_url: '',
    }, now: new Date() }, new AbortController().signal);
    expect(requests).toEqual(['https://test.salesforce.com/services/oauth2/token']);
    expect((result.credentials.access_token as SecretString).reveal()).toBe('minted');
    expect(result.credentials.instance_url).toBe('https://example.my.salesforce.com');
  });
});
