// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { JiraClient, type JiraClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return { oauth_client_id: credentials.oauthClientId, oauth_client_secret: credentials.oauthClientSecret.reveal(), access_token: credentials.accessToken.reveal(), refresh_token: credentials.refreshToken.reveal() };
}

/** Open one static Jira connection, including shared and private OAuth credentials. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: JiraClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'jira' && entry.connectionName === name);
    if (!selected) throw new TypeError('Jira project connection is not configured');
    const { pollInterval, ...raw } = selected.configuration as Record<string, unknown>;
    const config: Config = ConfigSchema.parse({ ...raw, ...(pollInterval !== undefined ? { pollIntervalMs: pollIntervalMilliseconds(pollInterval) } : {}) }) as Config;
    return new JiraClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}

/** Read Dex Web's pollInterval, Go duration nanoseconds or Go duration text, as whole milliseconds. */
function pollIntervalMilliseconds(value: unknown): number {
  if (typeof value === 'number' && Number.isSafeInteger(value)) return Math.floor(value / 1_000_000);
  if (typeof value !== 'string') throw new TypeError('Jira pollInterval must be a Go duration');
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, 'μs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  let rest = value, total = 0;
  if (!rest) throw new TypeError('Jira pollInterval must be a Go duration');
  while (rest) {
    const match = /^(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)/.exec(rest);
    if (!match) throw new TypeError('Jira pollInterval must be a Go duration');
    total += Number(match[1]) * units[match[2]!]!;
    rest = rest.slice(match[0].length);
  }
  return Math.floor(total);
}
