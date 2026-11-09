// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { OpenSearchClient, type OpenSearchClientOptions } from './client.js';
import { newProjectConnection as generatedProjectConnection } from './generated/connection.js';
import { decodeCredentials, type Config, type Credentials } from './generated/models.js';

const settingsSchema = z.strictObject({
  clusterUrl: z.string(), timeout: z.union([z.string(), z.number().int()]).optional(),
  maxResponseBytes: z.number().int().optional(), maxDocumentBytes: z.number().int().optional(),
});

/** Bind one named OpenSearch connection declared in a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: OpenSearchClientOptions = {}) {
  return generatedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry =>
      entry.connectorId === 'opensearch' && entry.connectionName === name);
    if (!selected) throw new TypeError('OpenSearch project connection is not configured');
    const settings = settingsSchema.parse(selected.configuration);
    const timeoutMs = typeof settings.timeout === 'number' ? Math.ceil(settings.timeout / 1_000_000)
      : settings.timeout !== undefined ? parseDurationMilliseconds(settings.timeout) : undefined;
    const config: Config = {
      clusterUrl: settings.clusterUrl,
      ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}),
      ...(settings.maxDocumentBytes !== undefined ? { maxDocumentBytes: settings.maxDocumentBytes } : {}),
    };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        return decodeCredentials(await loadedProject.resolveCredential('opensearch', name, signal));
      },
    };
    return new OpenSearchClient(config, credentials, options);
  });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = {
    ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1_000, m: 60_000, h: 3_600_000,
  };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('OpenSearch timeout must be a positive Go duration');
    milliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0)
    throw new TypeError('OpenSearch timeout must resolve to positive whole milliseconds');
  return milliseconds;
}
