// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { HubSpotClient, type HubSpotClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return { auth_method: credentials.authMethodId ?? '', access_token: credentials.accessToken.reveal(),
    oauth_client_id: credentials.oauthClientId ?? '', oauth_client_secret: credentials.oauthClientSecret?.reveal() ?? '',
    refresh_token: credentials.refreshToken?.reveal() ?? '' };
}

/** Open a named HubSpot project connection with generated OAuth credential wiring. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: HubSpotClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'hubspot' && entry.connectionName === name);
    if (!selected) throw new TypeError('HubSpot project connection is not configured');
    const config = ConfigSchema.parse(selected.configuration);
    return new HubSpotClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}
