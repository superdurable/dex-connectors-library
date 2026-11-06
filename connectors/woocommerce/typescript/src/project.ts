// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { WooCommerceClient, type WooCommerceClientOptions } from './client.js';
import { newProjectConnection as generatedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

const settingsSchema = z.strictObject({ storeUrl: z.string(), timeout: z.union([z.string(), z.number().int()]).optional(), maxResponseBytes: z.number().int().optional() });
const credentialsSchema = z.object({ consumer_key: z.string().min(1), consumer_secret: z.string().min(1) });

/** Open a WooCommerce connection declared in a loaded Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: WooCommerceClientOptions = {}) {
  return generatedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'woocommerce' && entry.connectionName === name);
    if (!selected) throw new TypeError('WooCommerce project connection is not configured');
    const settings = settingsSchema.parse(selected.configuration);
    const timeoutMs = typeof settings.timeout === 'number' ? Math.ceil(settings.timeout / 1_000_000)
      : settings.timeout !== undefined ? parseDurationMilliseconds(settings.timeout) : undefined;
    const config: Config = { storeUrl: settings.storeUrl, ...(timeoutMs !== undefined ? { timeoutMs } : {}), ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}) };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(_call, signal) {
        const fields = credentialsSchema.parse(await loadedProject.resolveCredential('woocommerce', name, signal));
        return { consumerKey: new SecretString(fields.consumer_key), consumerSecret: new SecretString(fields.consumer_secret) };
      },
    };
    return new WooCommerceClient(config, credentials, options);
  });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1_000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('WooCommerce timeout must be a positive Go duration');
    milliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError('WooCommerce timeout must resolve to positive whole milliseconds');
  return milliseconds;
}
