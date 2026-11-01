// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { RefreshingCredentialProvider, SecretString } from '@superdurable/connectors-sdk';
export {
  GetMessageInputSchema, SendMessageInputSchema, ReplyToMessageInputSchema,
  MessageSchema, SendMessageOutputSchema,
} from './generated/models.js';
export type {
  GetMessageInput, SendMessageInput, ReplyToMessageInput, Message, SendMessageOutput,
} from './generated/models.js';

/** Stable Gmail connector identity. */
export const ConnectorID = 'gmail';
/** Provider identity used by named connections. */
export const ProviderID = 'google';
/** Google OAuth authorization method. */
export const GoogleOAuthAuthMethodID = 'google-oauth';
/** Google Workspace domain delegation authorization method. */
export const WorkspaceDomainDelegationAuthMethodID = 'workspace-domain-delegation';

/** Public configuration; omitted fields use the manifest defaults. */
export interface Config {
  /** Gmail API base URL. */ endpoint?: string;
  /** Complete API response limit in bytes. */ maxResponseBytes?: number;
  /** Encoded MIME message limit in bytes. */ maxMessageBytes?: number;
  /** Trigger poll interval in milliseconds. */ pollIntervalMs?: number;
  /** Number of recent messages per Trigger poll. */ pollPageSize?: number;
}

/** Credential material held outside Dex Flow state. */
export interface Credentials {
  /** Selected authorization method. */ authMethodId?: string;
  /** Google OAuth application identifier. */ oauthClientId?: string;
  /** Private Google OAuth application secret. */ oauthClientSecret?: SecretString;
  /** Private short-lived bearer token. */ accessToken: SecretString;
  /** Private long-lived OAuth renewal token. */ refreshToken?: SecretString;
  /** Verified sender or delegated mailbox. */ primaryEmail: string;
  /** Private Workspace service-account JSON. */ serviceAccountKey?: SecretString;
  /** Workspace user to impersonate. */ delegatedUser?: string;
}

/** Host-owned refreshing credential source required by the Gmail client. */
export type CredentialSource = RefreshingCredentialProvider<Credentials>;
