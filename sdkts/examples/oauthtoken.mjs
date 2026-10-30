// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString } from '../dist/index.js';
import { TokenEndpoint, isRefreshRequiredWithIssueTime } from '../dist/oauthtoken/index.js';

const endpoint = new TokenEndpoint({
  providerName: 'Example', url: 'https://auth.example.test/token',
  terminalErrorCodes: ['invalid_grant'],
  fetch: async () => Response.json({ access_token: 'new-access', expires_in: 3600, token_type: 'Bearer' }),
});
const token = await endpoint.exchangeRefreshToken(
  { id: 'app-id', secret: new SecretString('test-client-secret'), authenticationMethod: 'client_secret_post' },
  new SecretString('test-refresh-token'),
);
const issuedAt = new Date();
const expiresAt = new Date(issuedAt.getTime() + token.expiresInMs);
if (isRefreshRequiredWithIssueTime(true, issuedAt, expiresAt, issuedAt, 'refresh')) throw new Error('fresh token was counted as expired');
if (token.nextRefreshToken(new SecretString('test-refresh-token')).reveal() !== 'test-refresh-token') throw new Error('refresh token was lost');
