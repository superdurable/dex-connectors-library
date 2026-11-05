// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { Client } from './client.js';
import { newConnection as bindConnection, newProjectConnection as bindProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

/** Open a Dex Web Smartsheet connection with call-time credentials. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return bindProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'smartsheet' && entry.connectionName === name);
    if (!selected) throw new TypeError('Smartsheet project connection is not configured');
    const fields = z.strictObject({ baseUrl: z.string().optional(), timeout: z.union([z.string(), z.number().int()]).optional(), maxResponseBytes: z.number().int().optional() }).parse(selected.configuration);
    const timeoutMs = typeof fields.timeout === 'number' ? Math.ceil(fields.timeout / 1_000_000) : fields.timeout ? parseDurationMilliseconds(fields.timeout) : undefined;
    const config: Config = { ...(fields.baseUrl !== undefined ? { baseUrl: fields.baseUrl } : {}), ...(timeoutMs !== undefined ? { timeoutMs } : {}), ...(fields.maxResponseBytes !== undefined ? { maxResponseBytes: fields.maxResponseBytes } : {}) };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(_call, signal) {
        const privateFields = await loadedProject.resolveCredential('smartsheet', name, signal);
        const decoded = z.object({ api_key: z.string() }).parse(privateFields);
        return { apiKey: new SecretString(decoded.api_key) };
      },
    };
    return new Client(config, credentials);
  });
}

/** Bind a client to a named Smartsheet connection. */
export function newConnection(client: Client, connectionName: string) { return bindConnection(client, { provider: 'smartsheet', name: connectionName }); }

function parseDurationMilliseconds(value: string): number {
  let remaining = value, totalMilliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Smartsheet timeout must be a positive Go duration');
    totalMilliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(totalMilliseconds) || totalMilliseconds <= 0) throw new TypeError('Smartsheet timeout must resolve to positive whole milliseconds');
  return totalMilliseconds;
}
