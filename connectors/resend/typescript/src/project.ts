// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { ResendClient, type ResendClientOptions } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

const projectSettingsSchema = z.strictObject({
  baseUrl: z.string().optional(),
  timeout: z.union([z.string(), z.number().int()]).optional(),
  maxResponseBytes: z.number().int().optional(),
});

/** Bind one static Resend connection from a loaded Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: ResendClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'resend' && entry.connectionName === name);
    if (!selected) throw new TypeError('Resend project connection is not configured');
    const settings = projectSettingsSchema.parse(selected.configuration);
    const timeoutMs = typeof settings.timeout === 'number' ? Math.ceil(settings.timeout / 1_000_000)
      : settings.timeout !== undefined ? parseDurationMilliseconds(settings.timeout) : undefined;
    if (timeoutMs !== undefined && (!Number.isSafeInteger(timeoutMs) || timeoutMs <= 0)) {
      throw new TypeError('Resend timeout must resolve to positive whole milliseconds');
    }
    const config: Config = {
      ...(settings.baseUrl !== undefined ? { baseUrl: settings.baseUrl } : {}),
      ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}),
    };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('resend', name, signal);
        const decoded = z.object({ api_key: z.string().min(1) }).parse(fields);
        return { apiKey: new SecretString(decoded.api_key) };
      },
    };
    return new ResendClient(config, credentials, options);
  });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1_000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Resend timeout must be a positive Go duration');
    milliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError('Resend timeout must resolve to positive whole milliseconds');
  return milliseconds;
}
