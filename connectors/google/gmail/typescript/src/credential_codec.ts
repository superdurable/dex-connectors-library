// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import { GoogleOAuthAuthMethodID, WorkspaceDomainDelegationAuthMethodID, type Credentials } from './models.js';

const StoredCredentialsSchema = z.object({
  auth_method: z.string().optional(), oauth_client_id: z.string().optional(), oauth_client_secret: z.string().optional(),
  access_token: z.string().optional(), refresh_token: z.string().optional(), primary_email: z.string().optional(),
  service_account_key: z.string().optional(), delegated_user: z.string().optional(),
});

/** Decode a private project connection using the manifest's credential field names. */
export function decodeGmailCredentials(value: unknown): Credentials { return decodeCredentialFields(value, false); }

/** Decode a brokered connection whose host retains its refresh material. */
export function decodeGmailSharedCredentials(value: unknown): Credentials { return decodeCredentialFields(value, true); }

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

function decodeCredentialFields(value: unknown, isShared: boolean): Credentials {
  const fields = StoredCredentialsSchema.safeParse(value);
  if (!fields.success) throw new TypeError('Gmail credential fields are invalid');
  const stored = fields.data;
  const credentials: Credentials = {
    authMethodId: stored.auth_method || GoogleOAuthAuthMethodID,
    oauthClientId: stored.oauth_client_id ?? '', oauthClientSecret: new SecretString(stored.oauth_client_secret ?? ''),
    accessToken: new SecretString(stored.access_token ?? ''), refreshToken: new SecretString(stored.refresh_token ?? ''),
    primaryEmail: stored.primary_email ?? '', serviceAccountKey: new SecretString(stored.service_account_key ?? ''),
    delegatedUser: stored.delegated_user ?? '',
  };
  if (credentials.authMethodId === GoogleOAuthAuthMethodID) {
    if (!credentials.oauthClientId || !credentials.primaryEmail || (!isShared && (!credentials.oauthClientSecret?.reveal() || !credentials.refreshToken?.reveal())) || !credentials.accessToken.reveal()) {
      throw new TypeError('Gmail OAuth connection credentials are incomplete');
    }
  } else if (credentials.authMethodId === WorkspaceDomainDelegationAuthMethodID) {
    if (!credentials.delegatedUser || (isShared ? !credentials.accessToken.reveal() : !credentials.serviceAccountKey?.reveal())) {
      throw new TypeError('Gmail Workspace connection credentials are incomplete');
    }
  } else throw new TypeError('Gmail authorization method is invalid');
  return credentials;
}
