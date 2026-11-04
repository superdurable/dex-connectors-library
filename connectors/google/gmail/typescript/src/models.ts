// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { RefreshingCredentialProvider, SecretString } from '@superdurable/connectors-sdk';
import { z } from 'zod';
export {
  GetMessageInputSchema, ReplyToMessageInputSchema, MessageSchema,
} from './generated/models.js';
export type {
  GetMessageInput, ReplyToMessageInput, Message,
} from './generated/models.js';

/** Public send input requires at least an array value before operation validation. */
export const SendMessageInputSchema = z.strictObject({
  to: z.array(z.string()), subject: z.string(), textBody: z.string(), htmlBody: z.string().optional(),
});
/** Message input accepted by Gmail applications. */
export type SendMessageInput = z.infer<typeof SendMessageInputSchema>;

/** Public send output always includes the resolved recipient list. */
export const SendMessageOutputSchema = z.strictObject({
  sender: z.string(), recipients: z.array(z.string()), messageId: z.string(), threadId: z.string(),
});
/** Confirmed Gmail delivery result. */
export type SendMessageOutput = z.infer<typeof SendMessageOutputSchema>;

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
