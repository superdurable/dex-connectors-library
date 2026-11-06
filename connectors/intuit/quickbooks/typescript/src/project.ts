// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { QuickBooksClient, type QuickBooksClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return { client_id: credentials.clientId, client_secret: credentials.clientSecret.reveal(), access_token: credentials.accessToken.reveal(), refresh_token: credentials.refreshToken.reveal(), id_token: credentials.idToken?.reveal() ?? '' };
}

/** Open a named QuickBooks project connection with generated OAuth credential decoders. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: QuickBooksClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'quickbooks' && entry.connectionName === name);
    if (!selected) throw new TypeError('QuickBooks project connection is not configured');
    const settings = ConfigSchema.parse(selected.configuration);
    const config: Config = {
      ...(settings.environment !== undefined ? { environment: settings.environment } : {}),
      ...(settings.realmId !== undefined ? { realmId: settings.realmId } : {}),
      ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}),
    };
    return new QuickBooksClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}
