// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { QuickBooksClient, type QuickBooksClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return { client_id: credentials.clientId, client_secret: credentials.clientSecret.reveal(), access_token: credentials.accessToken.reveal(), refresh_token: credentials.refreshToken.reveal(), id_token: credentials.idToken?.reveal() ?? '' };
}

/**
 * Open a named QuickBooks project connection with generated OAuth credential decoders. options, such as an
 * invoiceChanged logger or a local provider URL for verification, apply to the connection's client.
 */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: QuickBooksClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'quickbooks' && entry.connectionName === name);
    if (!selected) throw new TypeError('QuickBooks project connection is not configured');
    const { pollInterval, ...rest } = selected.configuration as Record<string, unknown>;
    // Dex Web stores pollInterval as Go does, integer nanoseconds; blank or 0 keeps the default.
    const settings = ConfigSchema.parse({ ...rest, pollIntervalMs: pollIntervalMilliseconds(pollInterval) });
    const config: Config = {
      ...(settings.environment !== undefined ? { environment: settings.environment } : {}),
      ...(settings.realmId !== undefined ? { realmId: settings.realmId } : {}),
      ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}),
      ...(settings.pollIntervalMs !== undefined ? { pollIntervalMs: settings.pollIntervalMs } : {}),
    };
    return new QuickBooksClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}

// Go decodes pollInterval into a time.Duration, so only an integer count of nanoseconds is valid.
function pollIntervalMilliseconds(value: unknown): number | undefined {
  if (value === undefined || value === null || value === '' || value === 0) return undefined;
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('QuickBooks pollInterval must be a duration in integer nanoseconds');
  if (value < 0) throw new TypeError('QuickBooks pollInterval cannot be negative');
  return Math.ceil(value / 1_000_000);
}
