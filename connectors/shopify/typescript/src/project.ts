// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { ShopifyClient, type ShopifyClientOptions } from './client.js';
import { newProjectConnection as generatedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

const settingsSchema = z.strictObject({
  shopSubdomain: z.string(),
  timeout: z.union([z.string(), z.number().int()]).optional(),
  maxResponseBytes: z.number().int().optional(),
});

/** Open a named Shopify connection from a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: ShopifyClientOptions = {}) {
  return generatedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'shopify' && entry.connectionName === name);
    if (!selected) throw new TypeError('Shopify project connection is not configured');
    const fields = settingsSchema.parse(selected.configuration);
    const timeoutMs = typeof fields.timeout === 'number' ? Math.ceil(fields.timeout / 1_000_000)
      : fields.timeout !== undefined ? parseDurationMilliseconds(fields.timeout) : undefined;
    const config: Config = {
      shopSubdomain: fields.shopSubdomain,
      ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      ...(fields.maxResponseBytes !== undefined ? { maxResponseBytes: fields.maxResponseBytes } : {}),
    };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(_call, signal) {
        const fields = await loadedProject.resolveCredential('shopify', name, signal);
        if (typeof fields.access_token !== 'string' || !fields.access_token) throw new TypeError('Shopify token is unavailable');
        return { accessToken: new SecretString(fields.access_token) };
      },
    };
    return new ShopifyClient(config, credentials, options);
  });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Shopify timeout must be a positive Go duration');
    milliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError('Shopify timeout must resolve to positive whole milliseconds');
  return milliseconds;
}
