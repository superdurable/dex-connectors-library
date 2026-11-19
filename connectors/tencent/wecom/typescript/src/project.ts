// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { WeComClient } from './client.js';
import { newProjectConnection as generatedProjectConnection } from './generated/connection.js';
import { ConfigSchema, decodeCredentials, type Config, type Credentials } from './generated/models.js';

/** Open one named WeCom connection from Dex project configuration. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return generatedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'wecom' && entry.connectionName === name);
    if (!selected) throw new TypeError('WeCom project connection is not configured');
    const fields = selected.configuration as Record<string, unknown>;
    const config = ConfigSchema.parse({ ...fields, ...(fields.timeout !== undefined ? { timeoutMs: parseDurationMilliseconds(fields.timeout) } : {}) }) as Config;
    const credentials: CredentialProvider<Credentials> = {
      async resolve(_call, signal) { return decodeCredentials(await loadedProject.resolveCredential('wecom', name, signal)); },
    };
    return new WeComClient(config, credentials);
  });
}

function parseDurationMilliseconds(value: unknown): number {
  if (typeof value === 'number') return Math.ceil(value / 1_000_000);
  if (typeof value !== 'string') throw new TypeError('timeout must be a duration');
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const part = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!part) throw new TypeError('timeout must be a positive Go duration');
    milliseconds += Number(part[1]) * (units[part[2] ?? ''] ?? NaN);
    remaining = remaining.slice(part[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError('timeout must resolve to positive whole milliseconds');
  return milliseconds;
}
