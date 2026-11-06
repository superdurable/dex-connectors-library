// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Credentials } from './generated/models.js';
import { newProjectConnection as bindProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { PipedriveClient, type PipedriveClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return {
    auth_method: credentials.authMethodId || 'api-token',
    ...(credentials.apiToken ? { api_token: credentials.apiToken.reveal() } : {}),
    ...(credentials.oauthClientId ? { oauth_client_id: credentials.oauthClientId } : {}),
    ...(credentials.oauthClientSecret ? { oauth_client_secret: credentials.oauthClientSecret.reveal() } : {}),
    ...(credentials.accessToken ? { access_token: credentials.accessToken.reveal() } : {}),
    ...(credentials.refreshToken ? { refresh_token: credentials.refreshToken.reveal() } : {}),
    ...(credentials.apiDomain ? { api_domain: credentials.apiDomain } : {}),
  };
}

/** Bind a named project connection with generated private and shared OAuth decoders. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: PipedriveClientOptions = {}) {
  return bindProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'pipedrive' && entry.connectionName === name);
    if (!selected) throw new TypeError('Pipedrive project connection is not configured');
    const raw = selected.configuration as Record<string, unknown>;
    const config = ConfigSchema.parse(raw);
    const timeout = raw.timeout;
    return new PipedriveClient({
      ...(config.endpoint !== undefined ? { endpoint: config.endpoint } : {}),
      ...(config.maxResponseBytes !== undefined ? { maxResponseBytes: config.maxResponseBytes } : {}),
      ...(timeout !== undefined ? { timeout: parseDurationMilliseconds(timeout) } : {}),
    }, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}

function parseDurationMilliseconds(value: unknown): number {
  if (typeof value === 'number') return Math.ceil(value / 1_000_000);
  if (typeof value !== 'string') throw new TypeError('Pipedrive timeout must be a positive Go duration');
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60000, h: 3600000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Pipedrive timeout must be a positive Go duration');
    milliseconds += Number(component[1]) * units[component[2]!]!;
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError('Pipedrive timeout must resolve to positive whole milliseconds');
  return milliseconds;
}
