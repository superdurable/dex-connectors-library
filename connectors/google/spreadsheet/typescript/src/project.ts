// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { SpreadsheetClient, type SpreadsheetClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return {
    oauth_client_id: credentials.oauthClientId,
    oauth_client_secret: credentials.oauthClientSecret.reveal(),
    access_token: credentials.accessToken.reveal(),
    refresh_token: credentials.refreshToken.reveal(),
  };
}

/** Open a named Sheets connection with generated private and shared credential decoders. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: SpreadsheetClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'google-sheets' && entry.connectionName === name);
    if (!selected) throw new TypeError('Google Sheets project connection is not configured');
    const settings = ConfigSchema.parse(selected.configuration);
    const config = {
      ...(settings.endpoint !== undefined ? { endpoint: settings.endpoint } : {}),
      ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}),
      ...(settings.maxRows !== undefined ? { maxRows: settings.maxRows } : {}),
    };
    return new SpreadsheetClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}
