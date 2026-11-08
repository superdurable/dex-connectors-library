// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectCredentialProvider } from './generated/connection.js';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './models.js';
import { BoxClient, type BoxClientOptions } from './client.js';
import { newConnection } from './connection.js';

function encodeCredentials(credentials: Credentials): Record<string, string> {
  return {
    oauth_client_id: credentials.oauthClientId,
    oauth_client_secret: credentials.oauthClientSecret.reveal(),
    access_token: credentials.accessToken.reveal(),
    refresh_token: credentials.refreshToken.reveal(),
  };
}

/** Open one statically named renewable Box connection from Dex project configuration. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: BoxClientOptions = {}) {
  if (!project) throw new TypeError('Box project is required');
  const selected = project.configuration.connections.find(entry => entry.connectorId === 'box' && entry.connectionName === connectionName);
  if (!selected || selected.provider !== 'box') throw new TypeError('Box project connection is not configured');
  const config = ConfigSchema.parse(selected.configuration) as Config;
  const provider = newProjectCredentialProvider(project, connectionName, decodeCredentials, encodeCredentials, decodeSharedCredentials);
  return newConnection(new BoxClient(config, provider, options), { provider: 'box', name: connectionName });
}
