// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import type { Config, Credentials } from './generated/models.js';
import { ConfigSchema } from './generated/models.js';
import { ConnectorID } from './generated/definitions.js';
import { EmailClient } from './client.js';
import { newConnection } from './generated/connection.js';

export { EmailClient, MailFailure } from './client.js';
export type { EmailClientOptions } from './client.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export type { Operations } from './generated/connection.js';
export type { Config, Credentials, SearchMessagesInput, SearchMessagesOutput, GetMessageInput, Message, MessageReference, MessageSummary, EmailAddress, SendMessageInput, SentMessage, ReplyToMessageInput, MoveMessageInput, MovedMessage, SetFlagsInput, MessageFlags } from './generated/models.js';
export { SearchMessagesInputSchema, GetMessageInputSchema, SendMessageInputSchema, ReplyToMessageInputSchema, MoveMessageInputSchema, SetFlagsInputSchema } from './generated/models.js';

/** Open a Dex Web connection with secret values resolved per invocation. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  if (!project || !connectionName.trim()) throw new TypeError('email project connection is required');
  const configured = project.configuration.connections.find(entry => entry.connectorId === ConnectorID && entry.connectionName === connectionName);
  if (!configured) throw new TypeError('email project connection is not configured');
  const config: Config = ConfigSchema.parse(configured.configuration);
  const credentialProvider: CredentialProvider<Credentials> = {
    async resolve(_call, signal) {
      const fields = await project.resolveCredential(ConnectorID, connectionName, signal);
      // A ZodError marks unusable stored material, which the client treats as terminal.
      const decoded = z.object({ username: z.string(), password: z.string() }).parse(fields);
      return { username: decoded.username, password: new SecretString(decoded.password), ...(typeof fields.smtp_username === 'string' ? { smtpUsername: fields.smtp_username } : {}), ...(typeof fields.smtp_password === 'string' ? { smtpPassword: new SecretString(fields.smtp_password) } : {}) };
    },
  };
  return newConnection(new EmailClient(config, credentialProvider), { provider: ConnectorID, name: connectionName });
}
