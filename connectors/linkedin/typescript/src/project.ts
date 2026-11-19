// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { LinkedInClient, type LinkedInClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return {
    oauth_client_id: credentials.oauthClientId ?? '', oauth_client_secret: credentials.oauthClientSecret?.reveal() ?? '',
    access_token: credentials.accessToken.reveal(), refresh_token: credentials.refreshToken?.reveal() ?? '',
  };
}

/** Open a named LinkedIn connection using generated private and shared credential decoders. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: LinkedInClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'linkedin' && entry.connectionName === name);
    if (!selected) throw new TypeError('LinkedIn project connection is not configured');
    const raw = selected.configuration as Record<string, unknown>;
    const settings = ConfigSchema.parse({ ...raw,
      ...(raw.timeout !== undefined ? { timeoutMs: typeof raw.timeout === 'number' ? Math.ceil(raw.timeout / 1_000_000) : parseDurationMilliseconds(raw.timeout, 'timeout') } : {}),
    });
    const pollIntervalMs = pollIntervalMilliseconds(raw.pollInterval);
    const config: Config = {
      ...(settings.userInfoUrl !== undefined ? { userInfoUrl: settings.userInfoUrl } : {}),
      ...(settings.timeoutMs !== undefined ? { timeoutMs: settings.timeoutMs } : {}),
      ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}),
      ...(pollIntervalMs !== undefined ? { pollIntervalMs } : {}),
    };
    return new LinkedInClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}

/** Read the stored pollInterval: Go nanoseconds, as Dex Web saves a duration, or Go duration text. */
function pollIntervalMilliseconds(value: unknown): number | undefined {
  if (value === undefined || value === null) return undefined;
  if (typeof value === 'number') {
    if (!Number.isSafeInteger(value)) throw new TypeError('LinkedIn configuration pollInterval has an invalid type');
    return value / 1_000_000;
  }
  return parseDurationMilliseconds(value, 'pollInterval');
}

function parseDurationMilliseconds(value: unknown, field: string): number {
  if (typeof value !== 'string') throw new TypeError(`LinkedIn ${field} must be a positive Go duration`);
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1_000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError(`LinkedIn ${field} must be a positive Go duration`);
    milliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError(`LinkedIn ${field} must resolve to positive whole milliseconds`);
  return milliseconds;
}
