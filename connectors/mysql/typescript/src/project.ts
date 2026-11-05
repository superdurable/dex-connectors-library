// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { MySQLClient } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

const projectSettingsSchema = z.strictObject({
  host: z.string(), port: z.number().int().optional(), database: z.string(), user: z.string(),
  sslMode: z.string().optional(), connectTimeout: z.union([z.string(), z.number().int()]).optional(),
  statementTimeout: z.union([z.string(), z.number().int()]).optional(), maxRows: z.number().int().optional(),
  maxResponseBytes: z.number().int().optional(),
});

/** Bind a statically named project connection to a native MySQL client. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'mysql' && entry.connectionName === name);
    if (!selected) throw new TypeError('MySQL project connection is not configured');
    const settings = projectSettingsSchema.parse(selected.configuration);
    const config: Config = {
      host: settings.host, database: settings.database, user: settings.user,
      ...(settings.port !== undefined ? { port: settings.port } : {}),
      ...(settings.sslMode !== undefined ? { sslMode: settings.sslMode } : {}),
      ...(settings.connectTimeout !== undefined ? { connectTimeoutMs: parseDurationMilliseconds(settings.connectTimeout) } : {}),
      ...(settings.statementTimeout !== undefined ? { statementTimeoutMs: parseDurationMilliseconds(settings.statementTimeout) } : {}),
      ...(settings.maxRows !== undefined ? { maxRows: settings.maxRows } : {}),
      ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}),
    };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('mysql', name, signal);
        const decoded = z.object({ password: z.string().min(1) }).parse(fields);
        return { password: new SecretString(decoded.password) };
      },
    };
    return new MySQLClient(config, credentials);
  });
}

function parseDurationMilliseconds(duration: string | number): number {
  if (typeof duration === 'number') return Math.ceil(duration / 1_000_000);
  let remaining = duration;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60000, h: 3600000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('MySQL timeout must be a positive Go duration');
    milliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError('MySQL timeout must resolve to positive whole milliseconds');
  return milliseconds;
}
