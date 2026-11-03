// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString } from '../dist/index.js';
import { ManifestCredentialRefreshDriver } from '../dist/oauthtoken/index.js';

const auth = {
  type: 'oauth2', fields: [
    { name: 'client_id', type: 'string' }, { name: 'client_secret', type: 'secretString' },
    { name: 'access_token', type: 'secretString' }, { name: 'refresh_token', type: 'secretString' },
  ], oauth2: {
    tokenEndpoint: 'https://auth.example.test/token', clientIDCredential: 'client_id',
    clientSecretCredential: 'client_secret', scopes: ['read'], credentialMappings: [
      { credential: 'access_token', source: 'access_token' },
      { credential: 'refresh_token', source: 'refresh_token' },
    ],
  },
};
const driver = new ManifestCredentialRefreshDriver({
  providerName: 'Example', auth,
  credentialFields: { client_id: 'clientId', client_secret: 'clientSecret', access_token: 'accessToken', refresh_token: 'refreshToken' },
  fetch: async () => Response.json({ access_token: 'INERT_NEW_ACCESS', refresh_token: 'INERT_NEW_REFRESH', expires_in: 3600, token_type: 'Bearer', scope: 'read' }),
});
const credentials = {
  clientId: 'INERT_CLIENT', clientSecret: new SecretString('INERT_SECRET'),
  accessToken: new SecretString('INERT_OLD_ACCESS'), refreshToken: new SecretString('INERT_OLD_REFRESH'),
};
const result = await driver.refresh({ credentials, now: new Date() }, new AbortController().signal);
if (result.credentials.accessToken.reveal() !== 'INERT_NEW_ACCESS') throw new Error('access token was not refreshed');
if (result.credentials.refreshToken.reveal() !== 'INERT_NEW_REFRESH') throw new Error('refresh token was not rotated');
