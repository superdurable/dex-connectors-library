// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { ZohoDeskClient, type ZohoDeskClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return {
    auth_method: credentials.authMethodId ?? '', access_token: credentials.accessToken.reveal(),
    oauth_client_id: credentials.oauthClientId, oauth_client_secret: credentials.oauthClientSecret.reveal(),
    refresh_token: credentials.refreshToken.reveal(),
  };
}

/** Open one named Zoho Desk connection with generated private and shared OAuth decoding. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: ZohoDeskClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'zoho-desk' && entry.connectionName === name);
    if (!selected) throw new TypeError('Zoho Desk project connection is not configured');
    const config = ConfigSchema.parse(selected.configuration);
    return new ZohoDeskClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}
