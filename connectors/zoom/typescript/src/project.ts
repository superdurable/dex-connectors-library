// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { ZoomClient, type ZoomClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return { oauth_client_id: credentials.oauthClientId, oauth_client_secret: credentials.oauthClientSecret.reveal(),
    access_token: credentials.accessToken.reveal(), refresh_token: credentials.refreshToken.reveal() };
}

/** Open a named Zoom project connection with generated renewable credential decoders. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: ZoomClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'zoom' && entry.connectionName === name);
    if (!selected) throw new TypeError('Zoom project connection is not configured');
    const config = ConfigSchema.parse(selected.configuration);
    const pollIntervalMs = pollIntervalMilliseconds(selected.configuration.pollInterval);
    return new ZoomClient({ ...(config.endpoint !== undefined ? { endpoint: config.endpoint } : {}), ...(config.maxResponseBytes !== undefined ? { maxResponseBytes: config.maxResponseBytes } : {}),
      ...(pollIntervalMs !== undefined ? { pollIntervalMs } : {}) }, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}

/** Read the stored pollInterval as Go decodes a time.Duration: integer nanoseconds, as Dex Web saves it. */
function pollIntervalMilliseconds(value: unknown): number | undefined {
  if (value === undefined || value === null) return undefined;
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('Zoom configuration pollInterval has an invalid type');
  return value / 1_000_000;
}
