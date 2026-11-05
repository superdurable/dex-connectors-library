// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { VercelClient } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

/** Open a named Vercel connection in a loaded Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'vercel' && entry.connectionName === name);
    if (!selected) throw new TypeError('Vercel project connection is not configured');
    const fields = z.strictObject({ baseUrl: z.string().optional(), timeout: z.union([z.string(), z.number().int()]).optional(), maxResponseBytes: z.number().int().optional() }).parse(selected.configuration);
    const timeoutMs = typeof fields.timeout === 'number' ? Math.ceil(fields.timeout / 1_000_000) : fields.timeout ? parseDurationMilliseconds(fields.timeout) : undefined;
    const config: Config = { ...(fields.baseUrl !== undefined ? { baseUrl: fields.baseUrl } : {}), ...(timeoutMs !== undefined ? { timeoutMs } : {}), ...(fields.maxResponseBytes !== undefined ? { maxResponseBytes: fields.maxResponseBytes } : {}) };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(_call: Call, signal?: AbortSignal): Promise<Credentials> {
        const privateFields = await loadedProject.resolveCredential('vercel', name, signal);
        if (typeof privateFields.access_token !== 'string') throw new TypeError('Vercel access token is unavailable');
        return { accessToken: new SecretString(privateFields.access_token) };
      },
    };
    return new VercelClient(config, credentials);
  });
}
function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let total = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const match = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!match) throw new TypeError('Vercel timeout must be a positive Go duration');
    total += Number(match[1]) * (units[match[2] ?? ''] ?? NaN);
    remaining = remaining.slice(match[0].length);
  }
  if (!Number.isSafeInteger(total) || total <= 0) throw new TypeError('Vercel timeout must resolve to positive whole milliseconds');
  return total;
}
