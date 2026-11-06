// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ConnectionRefSchema } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { TeamsClient, type TeamsClientOptions } from './client.js';
import { newConnection, newProjectCredentialProvider, type Connection } from './generated/connection.js';
import { ConnectorID, ProviderID } from './generated/definitions.js';
import {
  ConfigSchema, decodeCredentials, decodeSharedCredentials,
  type Config, type Credentials,
} from './generated/models.js';

/** Open one named Teams connection from a loaded Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: TeamsClientOptions = {}): Connection {
  if (!project) throw new TypeError('Microsoft Teams connection requires loaded project configuration');
  const reference = ConnectionRefSchema.parse({ provider: ProviderID, name: connectionName });
  const selected = project.configuration.connections.find(entry => entry.connectorId === ConnectorID && entry.connectionName === connectionName);
  if (!selected || selected.provider !== ProviderID) throw new TypeError('Microsoft Teams project connection is not configured');
  const fields = ConfigSchema.parse(selected.configuration);
  const config: Config = {
    ...(fields.endpoint !== undefined ? { endpoint: fields.endpoint } : {}),
    ...(fields.maxResponseBytes !== undefined ? { maxResponseBytes: fields.maxResponseBytes } : {}),
    ...(fields.maxMessageBytes !== undefined ? { maxMessageBytes: fields.maxMessageBytes } : {}),
  };
  const credentials = newProjectCredentialProvider(project, connectionName, decodeCredentials, encodeCredentials, decodeSharedCredentials);
  return newConnection(new TeamsClient(config, credentials, options), reference);
}

/** Encode complete private material for atomic project credential replacement. */
export function encodeCredentials(credentials: Credentials): Record<string, string> {
  const fields = {
    access_token: credentials.accessToken.reveal(),
    oauth_client_id: credentials.oauthClientId,
    oauth_client_secret: credentials.oauthClientSecret.reveal(),
    refresh_token: credentials.refreshToken.reveal(),
  };
  decodeCredentials(fields);
  return fields;
}
