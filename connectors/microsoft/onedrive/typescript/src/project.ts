// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectCredentialProvider } from './generated/connection.js';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Credentials } from './generated/models.js';
import { OneDriveClient, type OneDriveClientOptions } from './client.js';
import { newConnection } from './connection.js';

function encodeCredentials(credentials: Credentials): Record<string, string> {
  const method = credentials.authMethodId || 'microsoft-oauth';
  if (method === 'microsoft-app-only') return { auth_method: method, tenant_id: credentials.tenantId ?? '', app_client_id: credentials.appClientId ?? '', app_client_secret: credentials.appClientSecret?.reveal() ?? '', access_token: credentials.accessToken?.reveal() ?? '' };
  return { auth_method: method, oauth_client_id: credentials.oauthClientId ?? '', oauth_client_secret: credentials.oauthClientSecret?.reveal() ?? '', access_token: credentials.accessToken?.reveal() ?? '', refresh_token: credentials.refreshToken?.reveal() ?? '' };
}

/** Open one statically named OneDrive connection from a loaded Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: OneDriveClientOptions = {}) {
  if (!project) throw new TypeError('OneDrive project is required');
  const entry = project.configuration.connections.find(connection => connection.connectorId === 'microsoft-onedrive' && connection.connectionName === connectionName);
  if (!entry || entry.provider !== 'microsoft') throw new TypeError('OneDrive project connection is not configured');
  const config = ConfigSchema.parse(entry.configuration) as import('./models.js').Config;
  const provider = newProjectCredentialProvider(project, connectionName, decodeCredentials, encodeCredentials, decodeSharedCredentials);
  return newConnection(new OneDriveClient(config, provider, options), { provider: 'microsoft', name: connectionName });
}
