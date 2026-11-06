// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { SlackClient, type SlackClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return {
    oauth_client_id: credentials.oauthClientId ?? '',
    oauth_client_secret: credentials.oauthClientSecret?.reveal() ?? '',
    bot_token: credentials.botToken.reveal(),
    bot_refresh_token: credentials.botRefreshToken?.reveal() ?? '',
    user_token: credentials.userToken.reveal(),
    user_refresh_token: credentials.userRefreshToken?.reveal() ?? '',
    app_token: credentials.appToken?.reveal() ?? '',
  };
}

/** Open a named Slack connection with generated private and shared credential decoders. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: SlackClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'slack' && entry.connectionName === name);
    if (!selected) throw new TypeError('Slack project connection is not configured');
    const settings = ConfigSchema.parse(selected.configuration);
    return new SlackClient({
      ...(settings.endpoint !== undefined ? { endpoint: settings.endpoint } : {}),
      ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}),
      ...(settings.maxMessageCharacters !== undefined ? { maxMessageCharacters: settings.maxMessageCharacters } : {}),
    }, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}
