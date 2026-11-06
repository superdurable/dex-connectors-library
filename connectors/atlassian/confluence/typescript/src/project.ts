// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { ConfluenceClient, type ConfluenceClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return {
    oauth_client_id: credentials.oauthClientId,
    oauth_client_secret: credentials.oauthClientSecret.reveal(),
    access_token: credentials.accessToken.reveal(),
    refresh_token: credentials.refreshToken.reveal(),
  };
}

/** Open a named Confluence connection with generated private and shared OAuth credential decoding. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: ConfluenceClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'confluence' && entry.connectionName === name);
    if (!selected) throw new TypeError('Confluence project connection is not configured');
    const settings = ConfigSchema.parse(selected.configuration);
    const config: Config = {
      ...(settings.cloudId !== undefined ? { cloudId: settings.cloudId } : {}),
      ...(settings.endpoint !== undefined ? { endpoint: settings.endpoint } : {}),
      ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}),
    };
    return new ConfluenceClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}
