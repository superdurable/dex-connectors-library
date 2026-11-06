// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectCredentialProvider, Connection } from './generated/connection.js';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './models.js';
import { ZohoClient } from './client.js';
import type { ZohoClientOptions } from './http.js';

/** Encode complete replacement credentials for the host's private store. */
export function encodeCredentials(credentials: Credentials): Record<string, string> {
  return { auth_method: credentials.authMethodId ?? 'zoho-us-oauth', oauth_client_id: credentials.oauthClientId,
    oauth_client_secret: credentials.oauthClientSecret.reveal(), access_token: credentials.accessToken.reveal(),
    refresh_token: credentials.refreshToken.reveal(), ...(credentials.apiDomain ? { api_domain: credentials.apiDomain } : {}) };
}

/** Open a statically named Zoho CRM connection from a loaded Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: ZohoClientOptions = {}): Connection {
  if (!project) throw new TypeError('Zoho CRM loaded project is required');
  const entry = project.configuration.connections.find(connection => connection.connectorId === 'zoho-crm' && connection.connectionName === connectionName && connection.provider === 'zoho');
  if (!entry) throw new TypeError('Zoho CRM project connection is not configured');
  const source = newProjectCredentialProvider(project, connectionName, decodeCredentials, encodeCredentials, decodeSharedCredentials);
  const parsedConfig = ConfigSchema.parse(entry.configuration);
  const config: Config = parsedConfig.maxResponseBytes === undefined ? {} : { maxResponseBytes: parsedConfig.maxResponseBytes };
  return new Connection({ provider: 'zoho', name: connectionName }, new ZohoClient(config, source, options));
}
