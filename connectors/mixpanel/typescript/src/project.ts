// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { MixpanelClient, type MixpanelClientOptions } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

const projectSettingsSchema = z.strictObject({
  baseUrl: z.string().optional(),
  exportBaseUrl: z.string().optional(),
  projectId: z.number().int().optional(),
  timeout: z.union([z.string(), z.number().int()]).optional(),
  maxResponseBytes: z.number().int().optional(),
});

/** Bind a static Mixpanel connection declared in a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: MixpanelClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'mixpanel' && entry.connectionName === name);
    if (!selected) throw new TypeError('Mixpanel project connection is not configured');
    const settings = projectSettingsSchema.parse(selected.configuration);
    if (typeof settings.timeout === 'number' && settings.timeout <= 0) throw new TypeError('Mixpanel timeout must be positive');
    const timeoutMs = typeof settings.timeout === 'number' ? Math.ceil(settings.timeout / 1_000_000)
      : settings.timeout !== undefined ? parseDurationMilliseconds(settings.timeout) : undefined;
    const config: Config = {
      ...(settings.baseUrl !== undefined ? { baseUrl: settings.baseUrl } : {}),
      ...(settings.exportBaseUrl !== undefined ? { exportBaseUrl: settings.exportBaseUrl } : {}),
      ...(settings.projectId !== undefined ? { projectId: settings.projectId } : {}),
      ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}),
    };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('mixpanel', name, signal);
        const decoded = z.object({ service_account_username: z.string().min(1), service_account_secret: z.string().min(1) }).parse(fields);
        return { serviceAccountUsername: new SecretString(decoded.service_account_username), serviceAccountSecret: new SecretString(decoded.service_account_secret) };
      },
    };
    return new MixpanelClient(config, credentials, options);
  });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1_000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Mixpanel timeout must be a positive Go duration');
    milliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError('Mixpanel timeout must resolve to positive whole milliseconds');
  return milliseconds;
}
