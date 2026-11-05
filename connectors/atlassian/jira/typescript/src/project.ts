// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { JiraClient, type JiraClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return { oauth_client_id: credentials.oauthClientId, oauth_client_secret: credentials.oauthClientSecret.reveal(), access_token: credentials.accessToken.reveal(), refresh_token: credentials.refreshToken.reveal() };
}

/** Open one static Jira connection, including shared and private OAuth credentials. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: JiraClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'jira' && entry.connectionName === name);
    if (!selected) throw new TypeError('Jira project connection is not configured');
    const raw = selected.configuration as Record<string, unknown>;
    const config: Config = ConfigSchema.parse(raw) as Config;
    return new JiraClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}
