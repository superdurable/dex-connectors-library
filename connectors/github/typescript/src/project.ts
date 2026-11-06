// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './generated/models.js';
import { newProjectConnection as bindProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { GitHubClient, type GitHubClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return {
    oauth_client_id: credentials.oauthClientId,
    oauth_client_secret: credentials.oauthClientSecret.reveal(),
    access_token: credentials.accessToken.reveal(),
    refresh_token: credentials.refreshToken?.reveal() ?? '',
  };
}

/** Bind a named project connection to generated OAuth credential persistence. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: GitHubClientOptions = {}) {
  return bindProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'github' && entry.connectionName === name);
    if (!selected) throw new TypeError('GitHub project connection is not configured');
    const raw = selected.configuration as Record<string, unknown>;
    const config = ConfigSchema.parse({
      ...raw,
      ...(raw.timeout !== undefined ? { timeoutMs: typeof raw.timeout === 'number' ? Math.ceil(raw.timeout / 1_000_000) : parseDurationMilliseconds(raw.timeout) } : {}),
    }) as Config;
    return new GitHubClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}

function parseDurationMilliseconds(value: unknown): number {
  if (typeof value !== 'string') throw new TypeError('GitHub timeout must be a positive Go duration');
  let remaining = value, milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1_000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('GitHub timeout must be a positive Go duration');
    milliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError('GitHub timeout must resolve to positive whole milliseconds');
  return milliseconds;
}
