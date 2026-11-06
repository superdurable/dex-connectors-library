// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { Client, ListProjectsOperation, ListTimeEntriesOperation } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

export * from './generated/models.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { GeneratedMock, listProjectsDefault, listProjectsInvalidKey, listProjectsRetry, listTimeEntriesDefault, listTimeEntriesInvalidKey, listTimeEntriesRetry } from './generated/mock.js';
export { Client, ListProjectsOperation, ListTimeEntriesOperation };

/** Bind the declared Harvest connection and resolve its current secret per call. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'harvest' && entry.connectionName === name);
    if (!selected) throw new TypeError('Harvest project connection is not configured');
    const fields = z.object({ baseUrl: z.string().optional(), timeout: z.union([z.string(), z.number().int()]).optional(), maxResponseBytes: z.number().int().optional(), accountId: z.string() }).parse(selected.configuration);
    const timeoutMs = typeof fields.timeout === 'number' ? Math.ceil(fields.timeout / 1_000_000) : fields.timeout ? parseDurationMilliseconds(fields.timeout) : undefined;
    const config: Config = { accountId: fields.accountId, ...(fields.baseUrl !== undefined ? { baseUrl: fields.baseUrl } : {}), ...(timeoutMs !== undefined ? { timeoutMs } : {}), ...(fields.maxResponseBytes !== undefined ? { maxResponseBytes: fields.maxResponseBytes } : {}) };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(_call: Call, signal?: AbortSignal) {
        const privateFields = await loadedProject.resolveCredential('harvest', name, signal);
        if (typeof privateFields.api_key !== 'string') throw new TypeError('Harvest API key is unavailable');
        return { apiKey: new SecretString(privateFields.api_key) };
      },
    };
    return new Client(config, credentials);
  });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let totalMilliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Harvest timeout must be a positive Go duration');
    totalMilliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(totalMilliseconds) || totalMilliseconds <= 0) throw new TypeError('Harvest timeout must resolve to positive whole milliseconds');
  return totalMilliseconds;
}
