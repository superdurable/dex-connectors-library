// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { WorkspaceAdminClient, type WorkspaceAdminClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return {
    auth_method: credentials.authMethodId ?? '',
    service_account_key: credentials.serviceAccountKey?.reveal() ?? '', delegated_user: credentials.delegatedUser ?? '',
    oauth_client_id: credentials.oauthClientId ?? '', oauth_client_secret: credentials.oauthClientSecret?.reveal() ?? '',
    access_token: credentials.accessToken?.reveal() ?? '', refresh_token: credentials.refreshToken?.reveal() ?? '',
  };
}

/** Open a named Google Workspace Admin project connection with generated renewable credential decoders. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: WorkspaceAdminClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'google-workspace-admin' && entry.connectionName === name);
    if (!selected) throw new TypeError('Google Workspace Admin project connection is not configured');
    const settings = ConfigSchema.parse(selected.configuration);
    const config: Config = {
      ...(settings.endpoint !== undefined ? { endpoint: settings.endpoint } : {}),
      ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}),
      ...(settings.listUsersPageSize !== undefined ? { listUsersPageSize: settings.listUsersPageSize } : {}),
    };
    return new WorkspaceAdminClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}
