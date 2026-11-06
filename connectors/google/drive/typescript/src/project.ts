// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newConnection, newProjectCredentialProvider, type Connection } from './generated/connection.js';
import { ConnectorID, ProviderID } from './generated/definitions.js';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './generated/models.js';
import { DriveClient, type DriveClientOptions } from './client.js';

/** Bind a configured Google Drive connection and generated credential decoders. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: DriveClientOptions = {}): Connection {
  if (!project) throw new TypeError('Google Drive project is required');
  const entry = project.configuration.connections.find(connection => connection.connectorId === ConnectorID && connection.connectionName === connectionName);
  if (!entry || entry.provider !== ProviderID) throw new TypeError('Google Drive project connection is not configured');
  const fields = ConfigSchema.parse(entry.configuration);
  const config: Config = { ...fields };
  const provider = newProjectCredentialProvider(project, connectionName, decodeCredentials, encodeCredentials, decodeSharedCredentials);
  return newConnection(new DriveClient(config, provider, options), { provider: ProviderID, name: connectionName });
}

/** Encode private replacement material using generated credential field names. */
export function encodeCredentials(credentials: Credentials): Record<string, string> {
  const fields: Record<string, string> = { auth_method: credentials.authMethodId ?? 'google-oauth' };
  for (const [name, value] of [
    ['access_token', credentials.accessToken?.reveal()], ['delegated_user', credentials.delegatedUser],
    ['oauth_client_id', credentials.oauthClientId], ['oauth_client_secret', credentials.oauthClientSecret?.reveal()],
    ['refresh_token', credentials.refreshToken?.reveal()], ['service_account_key', credentials.serviceAccountKey?.reveal()],
  ] as const) if (value) fields[name] = value;
  return fields;
}
