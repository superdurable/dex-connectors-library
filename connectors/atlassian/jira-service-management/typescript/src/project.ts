// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { JiraServiceManagementClient, type JiraClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return { oauth_client_id: credentials.oauthClientId, oauth_client_secret: credentials.oauthClientSecret.reveal(), access_token: credentials.accessToken.reveal(), refresh_token: credentials.refreshToken.reveal() };
}

/** Open a named Jira Service Management connection with generated OAuth decoders. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: JiraClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'jira-service-management' && entry.connectionName === name);
    if (!selected) throw new TypeError('Jira Service Management project connection is not configured');
    const config = ConfigSchema.parse(selected.configuration);
    return new JiraServiceManagementClient({ ...(config.cloudId !== undefined ? { cloudId: config.cloudId } : {}), ...(config.endpoint !== undefined ? { endpoint: config.endpoint } : {}), ...(config.maxResponseBytes !== undefined ? { maxResponseBytes: config.maxResponseBytes } : {}) }, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}
