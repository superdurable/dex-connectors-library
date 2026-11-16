// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { XeroClient, type XeroClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return {
    auth_method: credentials.authMethodId ?? 'custom-connection',
    client_id: credentials.clientId,
    client_secret: credentials.clientSecret?.reveal() ?? '',
    access_token: credentials.accessToken?.reveal() ?? '',
    refresh_token: credentials.refreshToken?.reveal() ?? '',
  };
}

/**
 * Open a named Xero connection with generated private and shared OAuth credential decoding. options, such as an
 * invoiceChanged logger or a local provider URL for verification, apply to the connection's client.
 */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: XeroClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'xero' && entry.connectionName === name);
    if (!selected) throw new TypeError('Xero project connection is not configured');
    const { pollInterval, ...rest } = selected.configuration as Record<string, unknown>;
    // Dex Web stores pollInterval as Go does, integer nanoseconds; blank or 0 keeps the default.
    const parsed = ConfigSchema.parse({ ...rest, pollIntervalMs: pollIntervalMilliseconds(pollInterval) });
    const config = { ...(parsed.maxResponseBytes !== undefined ? { maxResponseBytes: parsed.maxResponseBytes } : {}), ...(parsed.pollIntervalMs !== undefined ? { pollIntervalMs: parsed.pollIntervalMs } : {}), ...(parsed.organisation !== undefined ? { organisation: parsed.organisation } : {}) };
    return new XeroClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}

// Go decodes pollInterval into a time.Duration, so only an integer count of nanoseconds is valid.
function pollIntervalMilliseconds(value: unknown): number | undefined {
  if (value === undefined || value === null || value === '' || value === 0) return undefined;
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('Xero pollInterval must be a duration in integer nanoseconds');
  if (value < 0) throw new TypeError('Xero pollInterval cannot be negative');
  return Math.ceil(value / 1_000_000);
}
