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
  let nanoseconds = 0n;
  const maximumNanoseconds = (1n << 63n) - 1n;
  const units: Record<string, bigint> = { ns: 1n, us: 1_000n, 'µs': 1_000n, 'μs': 1_000n, ms: 1_000_000n, s: 1_000_000_000n, m: 60_000_000_000n, h: 3_600_000_000_000n };
  while (remaining) {
    const component = /^(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Todoist timeout must be a positive Go duration');
    const [wholePart, fractionalPart] = (component[1] ?? '').split('.');
    const unit = units[component[2] ?? ''];
    if (unit === undefined) throw new TypeError('Todoist timeout must be a positive Go duration');
    let componentNanoseconds = BigInt(wholePart || '0') * unit;
    if (fractionalPart) {
      let fraction = 0n;
      let scale = 1;
      for (const digit of fractionalPart) {
        if (fraction > maximumNanoseconds / 10n || fraction * 10n + BigInt(digit) > 1n << 63n) break;
        fraction = fraction * 10n + BigInt(digit);
        scale *= 10;
      }
      // Go converts the fractional digits to float64 before truncating their nanoseconds.
      componentNanoseconds += BigInt(Math.trunc(Number(fraction) * (Number(unit) / scale)));
    }
    nanoseconds += componentNanoseconds;
    if (nanoseconds > maximumNanoseconds) throw new TypeError('Todoist timeout exceeds a Go duration');
    remaining = remaining.slice(component[0].length);
  }
  if (nanoseconds <= 0n) throw new TypeError('Todoist timeout must resolve to positive milliseconds');
  return Number((nanoseconds + 999_999n) / 1_000_000n);
}
