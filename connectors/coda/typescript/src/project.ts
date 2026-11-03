// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import type { Config, Credentials } from './generated/models.js';
import { CodaClient } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';

/** Bind one named Coda connection from a private Dex project configuration. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'coda' && entry.connectionName === name);
    if (!selected) throw new TypeError('Coda project connection is not configured');
    const fields = z.strictObject({
      baseUrl: z.string().optional(),
      timeout: z.union([z.string(), z.number().int()]).optional(),
      maxResponseBytes: z.number().int().optional(),
    }).parse(selected.configuration);
    const timeoutMs = typeof fields.timeout === 'number'
      ? Math.ceil(fields.timeout / 1_000_000)
      : fields.timeout ? parseDurationMilliseconds(fields.timeout) : undefined;
    const config: Config = {
      ...(fields.baseUrl !== undefined ? { baseUrl: fields.baseUrl } : {}),
      ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      ...(fields.maxResponseBytes !== undefined ? { maxResponseBytes: fields.maxResponseBytes } : {}),
    };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('coda', name, signal);
        const token = fields.api_token;
        if (typeof token !== 'string' || !token) throw new TypeError('Coda API token is unavailable');
        return { apiToken: new SecretString(token) };
      },
    };
    return new CodaClient(config, credentials);
  });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let totalMilliseconds = 0;
  const unitMilliseconds: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Coda timeout must be a positive Go duration');
    totalMilliseconds += Number(component[1]) * (unitMilliseconds[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(totalMilliseconds) || totalMilliseconds <= 0) throw new TypeError('Coda timeout must resolve to positive whole milliseconds');
  return totalMilliseconds;
}
