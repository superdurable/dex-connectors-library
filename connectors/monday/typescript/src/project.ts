// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { MondayClient, type MondayClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return {
    auth_method: credentials.authMethodId ?? 'personal-api-token',
    ...(credentials.authMethodId === 'monday-oauth'
      ? { oauth_client_id: credentials.oauthClientId ?? '', oauth_client_secret: credentials.oauthClientSecret?.reveal() ?? '', access_token: credentials.accessToken?.reveal() ?? '', refresh_token: credentials.refreshToken?.reveal() ?? '' }
      : { api_token: credentials.apiToken?.reveal() ?? '' }),
  };
}

/** Open one named monday.com connection with generated private and shared decoders. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: MondayClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'monday' && entry.connectionName === name);
    if (!selected) throw new TypeError('monday.com project connection is not configured');
    const config = ConfigSchema.parse(selected.configuration);
    const pollIntervalMs = pollIntervalMilliseconds(selected.configuration.pollInterval);
    return new MondayClient({ ...(config.maxResponseBytes === undefined ? {} : { maxResponseBytes: config.maxResponseBytes }), ...(pollIntervalMs === undefined ? {} : { pollIntervalMs }) }, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}

/**
 * Read the stored pollInterval as Go decodes a time.Duration: integer nanoseconds, where null or zero uses the
 * default; any other JSON value is invalid.
 */
function pollIntervalMilliseconds(value: unknown): number | undefined {
  if (value === undefined || value === null) return undefined;
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('monday.com configuration pollInterval has an invalid type');
  return value === 0 ? undefined : value / 1_000_000;
}
