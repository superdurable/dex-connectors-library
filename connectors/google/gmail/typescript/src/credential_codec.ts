// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString } from '@superdurable/connectors-sdk';
import { decodeCredentials, decodeSharedCredentials, type Credentials as GeneratedCredentials } from './generated/models.js';
import { GoogleOAuthAuthMethodID, type Credentials } from './models.js';

/** Decode a private project connection using the manifest's credential field names. */
export function decodeGmailCredentials(value: unknown): Credentials { return completeCredentials(decodeGmailFields(value, false)); }

/** Decode a brokered connection whose host retains its refresh material. */
export function decodeGmailSharedCredentials(value: unknown): Credentials { return completeCredentials(decodeGmailFields(value, true)); }

/** Encode complete replacement credentials only for the private project store. */
export function encodeGmailCredentials(credentials: Credentials): Record<string, string> {
  const fields: Record<string, string> = { auth_method: credentials.authMethodId ?? GoogleOAuthAuthMethodID };
  for (const [name, value] of [
    ['oauth_client_id', credentials.oauthClientId], ['oauth_client_secret', credentials.oauthClientSecret?.reveal()],
    ['access_token', credentials.accessToken.reveal()], ['refresh_token', credentials.refreshToken?.reveal()],
    ['primary_email', credentials.primaryEmail], ['service_account_key', credentials.serviceAccountKey?.reveal()],
    ['delegated_user', credentials.delegatedUser],
  ] as const) if (value) fields[name] = value;
  return fields;
}

function completeCredentials(decoded: GeneratedCredentials): Credentials {
  return {
    authMethodId: decoded.authMethodId ?? GoogleOAuthAuthMethodID,
    oauthClientId: decoded.oauthClientId ?? '', oauthClientSecret: decoded.oauthClientSecret ?? new SecretString(''),
    accessToken: decoded.accessToken ?? new SecretString(''), refreshToken: decoded.refreshToken ?? new SecretString(''),
    primaryEmail: decoded.primaryEmail ?? '', serviceAccountKey: decoded.serviceAccountKey ?? new SecretString(''),
    delegatedUser: decoded.delegatedUser ?? '',
  };
}

function decodeGmailFields(value: unknown, isShared: boolean): GeneratedCredentials {
  try { return isShared ? decodeSharedCredentials(value) : decodeCredentials(value); }
  catch (error) {
    const message = error instanceof Error ? error.message : '';
    if (message.includes('is required')) {
      const method = value && typeof value === 'object' && 'auth_method' in value ? value.auth_method : '';
      throw new TypeError(method === 'workspace-domain-delegation'
        ? 'Gmail Workspace connection credentials are incomplete' : 'Gmail OAuth connection credentials are incomplete');
    }
    if (message.includes('auth_method is invalid')) throw new TypeError('Gmail authorization method is invalid');
    throw new TypeError('Gmail credential fields are invalid');
  }
}
