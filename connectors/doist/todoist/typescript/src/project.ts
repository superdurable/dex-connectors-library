// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { TodoistClient, type TodoistClientOptions } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

/** Open a static Todoist connection declared in a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: TodoistClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'todoist' && entry.connectionName === name);
    if (!selected) throw new TypeError('Todoist project connection is not configured');
    const settings = z.object({ baseUrl: z.string().optional(), timeout: z.union([z.string(), z.number()]).optional(), maxResponseBytes: z.number().int().optional() }).parse(selected.configuration);
    const timeoutMs = typeof settings.timeout === 'number' ? Math.ceil(settings.timeout / 1_000_000)
      : settings.timeout !== undefined ? parseDurationMilliseconds(settings.timeout) : undefined;
    const config: Config = { ...(settings.baseUrl !== undefined ? { baseUrl: settings.baseUrl } : {}),
      ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}) };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('todoist', name, signal);
        const decoded = z.object({ api_token: z.string().min(1) }).parse(fields);
        return { apiToken: new SecretString(decoded.api_token) };
      },
    };
    return new TodoistClient(config, credentials, options);
  });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value.startsWith('+') ? value.slice(1) : value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, 'μs': 0.001, ms: 1, s: 1_000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Todoist timeout must be a positive Go duration');
    milliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isFinite(milliseconds) || milliseconds <= 0 || !Number.isSafeInteger(Math.ceil(milliseconds))) throw new TypeError('Todoist timeout must resolve to positive milliseconds');
  return Math.max(1, Math.ceil(milliseconds));
}
