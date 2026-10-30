// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import type { RefreshingCredentialProvider, SecretString } from '@superdurable/connectors-sdk';

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

/** Input for one Gmail message read. */
export const GetMessageInputSchema = z.strictObject({ messageId: z.string() });
/** Input for one Gmail message read. */
export type GetMessageInput = z.infer<typeof GetMessageInputSchema>;

/** Input for one outbound Gmail message. */
export const SendMessageInputSchema = z.strictObject({
  to: z.array(z.string()), subject: z.string(), textBody: z.string(), htmlBody: z.string().optional(),
});
/** Input for one outbound Gmail message. */
export type SendMessageInput = z.infer<typeof SendMessageInputSchema>;

/** Input for a reply to an existing Gmail message. */
export const ReplyToMessageInputSchema = z.strictObject({
  messageId: z.string(), textBody: z.string(), htmlBody: z.string().optional(),
});
/** Input for a reply to an existing Gmail message. */
export type ReplyToMessageInput = z.infer<typeof ReplyToMessageInputSchema>;

/** Decoded Gmail message returned by getMessage. */
export const MessageSchema = z.strictObject({
  messageId: z.string(), threadId: z.string(), rfcMessageId: z.string().optional(),
  inReplyTo: z.string().optional(), references: z.string().optional(), from: z.string(),
  replyTo: z.string().optional(), to: z.array(z.string()).optional(), subject: z.string(),
  textBody: z.string().optional(), htmlBody: z.string().optional(), snippet: z.string().optional(),
  receivedAt: z.iso.datetime({ offset: true }), labelIds: z.array(z.string()).optional(),
});
/** Decoded Gmail message returned by getMessage. */
export type Message = z.infer<typeof MessageSchema>;

/** Gmail accepted-send identity and safe recipients. */
export const SendMessageOutputSchema = z.strictObject({
  sender: z.string(), recipients: z.array(z.string()), messageId: z.string(), threadId: z.string(),
});
/** Gmail accepted-send identity and safe recipients. */
export type SendMessageOutput = z.infer<typeof SendMessageOutputSchema>;
