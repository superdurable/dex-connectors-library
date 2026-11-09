// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { WordPressClient, type WordPressClientOptions } from './client.js';
import { newProjectConnection as generatedProjectConnection } from './generated/connection.js';
import { decodeCredentials, type Config, type Credentials } from './generated/models.js';

const settingsSchema = z.strictObject({
  siteUrl: z.string(), timeout: z.union([z.string(), z.number().int()]).optional(),
  maxResponseBytes: z.number().int().optional(), maxPostBytes: z.number().int().optional(),
});

/** Bind one named WordPress connection declared in a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: WordPressClientOptions = {}) {
  return generatedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry =>
      entry.connectorId === 'wordpress' && entry.connectionName === name);
    if (!selected) throw new TypeError('WordPress project connection is not configured');
    const settings = settingsSchema.parse(selected.configuration);
    const timeoutMs = typeof settings.timeout === 'number' ? Math.ceil(settings.timeout / 1_000_000)
      : settings.timeout !== undefined ? parseDurationMilliseconds(settings.timeout) : undefined;
    const config: Config = {
      siteUrl: settings.siteUrl,
      ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}),
      ...(settings.maxPostBytes !== undefined ? { maxPostBytes: settings.maxPostBytes } : {}),
    };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        return decodeCredentials(await loadedProject.resolveCredential('wordpress', name, signal));
      },
    };
    return new WordPressClient(config, credentials, options);
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
    if (!component) throw new TypeError('WordPress timeout must be a positive Go duration');
    milliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0)
    throw new TypeError('WordPress timeout must resolve to positive whole milliseconds');
  return milliseconds;
}
