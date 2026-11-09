// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { MicrosoftTodoClient } from './client.js';
import { newProjectConnection as generatedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './generated/models.js';

/** Encode renewable Microsoft OAuth material for atomic project persistence. */
export function encodeCredentials(credentials: Credentials): Record<string, string> {
  return {
    oauth_client_id: credentials.oauthClientId,
    oauth_client_secret: credentials.oauthClientSecret.reveal(),
    access_token: credentials.accessToken.reveal(),
    refresh_token: credentials.refreshToken.reveal(),
  };
}

/** Open one statically named Microsoft To Do project connection. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return generatedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'microsoft-todo' && entry.connectionName === name);
    if (!selected) throw new TypeError('Microsoft To Do project connection is not configured');
    const limit = (selected.configuration as Record<string, unknown>).maxResponseBytes;
    if (limit !== undefined && (typeof limit !== 'number' || !Number.isSafeInteger(limit)))
      throw new TypeError('Microsoft To Do maxResponseBytes must be an integer');
    const config: Config = { ...(typeof limit === 'number' ? { maxResponseBytes: limit } : {}) };
    const credentials = newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials);
    return new MicrosoftTodoClient(config, credentials);
  });
}
