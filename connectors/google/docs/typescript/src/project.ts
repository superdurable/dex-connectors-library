// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConnectionRefSchema } from '@superdurable/connectors-sdk';
import { DocsClient, type DocsClientOptions } from './client.js';
import { newConnection, newProjectCredentialProvider } from './generated/connection.js';
import { ConnectorID, ProviderID } from './generated/definitions.js';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Credentials } from './generated/models.js';

/** Open a statically named Google Docs connection from a loaded Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: DocsClientOptions = {}) {
  if (!project) throw new TypeError('Google Docs project configuration is required');
  const reference = ConnectionRefSchema.parse({ provider: ProviderID, name: connectionName });
  const entry = project.configuration.connections.find(connection => connection.connectorId === ConnectorID && connection.connectionName === connectionName);
  if (!entry || entry.provider !== ProviderID) throw new TypeError('Google Docs project connection is not configured');
  const config = ConfigSchema.parse(entry.configuration);
  const provider = newProjectCredentialProvider(project, connectionName, decodeCredentials, encodeCredentials, decodeSharedCredentials);
  return newConnection(new DocsClient(config, provider, options), reference);
}

/** Encode the manifest fields for one private credential replacement. */
export function encodeCredentials(credentials: Credentials): Record<string, string> {
  const fields: Record<string, string> = { auth_method: credentials.authMethodId || 'google-oauth' };
  for (const [name, value] of [
    ['access_token', credentials.accessToken?.reveal()], ['delegated_user', credentials.delegatedUser],
    ['oauth_client_id', credentials.oauthClientId], ['oauth_client_secret', credentials.oauthClientSecret?.reveal()],
    ['refresh_token', credentials.refreshToken?.reveal()], ['service_account_key', credentials.serviceAccountKey?.reveal()],
  ] as const) if (value) fields[name] = value;
  return fields;
}
