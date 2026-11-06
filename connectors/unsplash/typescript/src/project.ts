// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { Client, type UnsplashClientOptions } from './client.js';
import { newProjectConnection as generatedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

const settingsSchema = z.strictObject({ baseUrl: z.string().optional(), timeout: z.union([z.string(), z.number().int()]).optional(), maxResponseBytes: z.number().int().optional() });
/** Open declared public settings and resolve the current private access key per call. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: UnsplashClientOptions = {}) {
  return generatedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'unsplash' && entry.connectionName === name);
    if (!selected) throw new TypeError('Unsplash project connection is not configured');
    const settings = settingsSchema.parse(selected.configuration);
    const timeoutMs = typeof settings.timeout === 'number' ? Math.ceil(settings.timeout / 1_000_000) : settings.timeout === undefined ? undefined : parseDurationMilliseconds(settings.timeout);
    const config: Config = { ...(settings.baseUrl !== undefined ? { baseUrl: settings.baseUrl } : {}), ...(timeoutMs !== undefined ? { timeoutMs } : {}), ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}) };
    const credentials: CredentialProvider<Credentials> = { async resolve(_call, signal) {
      const fields = await loadedProject.resolveCredential('unsplash', name, signal);
      if (typeof fields.access_key !== 'string') throw new TypeError('Unsplash access key is unavailable');
      return { accessKey: new SecretString(fields.access_key) };
    } };
    return new Client(config, credentials, options);
  });
}
function parseDurationMilliseconds(value: string): number {
  let remaining = value, milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1_000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const match = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!match) throw new TypeError('Unsplash timeout must be a Go duration');
    milliseconds += Number(match[1]) * (units[match[2] ?? ''] ?? NaN);
    remaining = remaining.slice(match[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds < 0) throw new TypeError('Unsplash timeout is invalid');
  return milliseconds;
}
