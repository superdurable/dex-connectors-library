// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectCredentialProvider } from './generated/connection.js';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './models.js';
import { BasecampClient, type BasecampClientOptions } from './client.js';
import { newConnection } from './connection.js';

function encodeCredentials(credentials: Credentials): Record<string, string> {
  return {
    oauth_client_id: credentials.oauthClientId,
    oauth_client_secret: credentials.oauthClientSecret.reveal(),
    access_token: credentials.accessToken.reveal(),
    refresh_token: credentials.refreshToken.reveal(),
  };
}

/** Open a statically named renewable Accounts connection from Dex project configuration. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: BasecampClientOptions = {}) {
  if (!project) throw new TypeError('Accounts project is required');
  const selected = project.configuration.connections.find(entry => entry.connectorId === 'basecamp' && entry.connectionName === connectionName);
  if (!selected || selected.provider !== 'basecamp') throw new TypeError('Accounts project connection is not configured');
  const config = ConfigSchema.parse(selected.configuration) as Config;
  const provider = newProjectCredentialProvider(project, connectionName, decodeCredentials, encodeCredentials, decodeSharedCredentials);
  return newConnection(new BasecampClient(config, provider, options), { provider: 'basecamp', name: connectionName });
}
