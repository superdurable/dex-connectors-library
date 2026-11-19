// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { KlaviyoClient } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

/** Open a static Klaviyo connection declared in a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'klaviyo' && entry.connectionName === name);
    if (!selected) throw new TypeError('Klaviyo project connection is not configured');
    const config = z.object({ baseUrl: z.string().optional(), timeout: z.union([z.string(), z.number().int()]).optional(), maxResponseBytes: z.number().int().optional() }).parse(selected.configuration);
    const timeoutMs = typeof config.timeout === 'number' ? Math.ceil(config.timeout / 1_000_000) : config.timeout ? parseDurationMilliseconds(config.timeout) : undefined;
    const clientConfig: Config = { ...(config.baseUrl !== undefined ? { baseUrl: config.baseUrl } : {}), ...(timeoutMs !== undefined ? { timeoutMs } : {}), ...(config.maxResponseBytes !== undefined ? { maxResponseBytes: config.maxResponseBytes } : {}) };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(_call, signal) {
        const fields = await loadedProject.resolveCredential('klaviyo', name, signal);
        const decoded = z.object({ private_api_key: z.string().min(1) }).parse(fields);
        return { privateApiKey: new SecretString(decoded.private_api_key) };
      },
    };
    return new KlaviyoClient(clientConfig, credentials);
  });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Klaviyo timeout must be a positive Go duration');
    milliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError('Klaviyo timeout must resolve to positive milliseconds');
  return milliseconds;
}
