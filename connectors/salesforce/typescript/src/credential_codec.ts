// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString } from '@superdurable/connectors-sdk';
import type { Credentials } from './models.js';

/** Store complete replacement material in the private Dex project connection file. */
export function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  const method = credentials.authMethodId || 'salesforce-oauth';
  const result: Record<string, unknown> = { auth_method: method };
  if (credentials.oauthClientId) result.oauth_client_id = credentials.oauthClientId;
  if (credentials.accessToken instanceof SecretString && credentials.accessToken.reveal())
    result.access_token = credentials.accessToken.reveal();
  if (credentials.instanceUrl) result.instance_url = credentials.instanceUrl;
  if (method === 'salesforce-oauth' || method === 'salesforce-sandbox-oauth') {
    if (credentials.oauthClientSecret instanceof SecretString && credentials.oauthClientSecret.reveal())
      result.oauth_client_secret = credentials.oauthClientSecret.reveal();
    if (credentials.refreshToken instanceof SecretString && credentials.refreshToken.reveal())
      result.refresh_token = credentials.refreshToken.reveal();
  } else if (method === 'salesforce-jwt-bearer') {
    if (credentials.jwtUsername) result.jwt_username = credentials.jwtUsername;
    if (credentials.jwtPrivateKey instanceof SecretString && credentials.jwtPrivateKey.reveal())
      result.jwt_private_key = credentials.jwtPrivateKey.reveal();
    if (credentials.jwtLoginEnvironment) result.jwt_login_environment = credentials.jwtLoginEnvironment;
  } else throw new TypeError('Salesforce authorization method is invalid');
  return result;
}
