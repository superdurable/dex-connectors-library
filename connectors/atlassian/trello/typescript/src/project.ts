// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { TrelloClient, type TrelloClientOptions } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

/**
 * Open a named static Trello connection declared in a Dex project. options supply the cardChanged Trigger
 * logger and, in tests, the transport and clock.
 */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: TrelloClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'trello' && entry.connectionName === name);
    if (!selected) throw new TypeError('Trello project connection is not configured');
    const { pollInterval, ...raw } = selected.configuration as Record<string, unknown>;
    const config = z.object({ endpoint: z.string().optional(), maxResponseBytes: z.number().int().optional(), pollIntervalMs: z.number().int().optional() })
      .parse({ ...raw, ...(pollInterval !== undefined ? { pollIntervalMs: pollIntervalMilliseconds(pollInterval) } : {}) }) as Config;
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('trello', name, signal);
        const decoded = z.object({ api_key: z.string().min(1), token: z.string().min(1) }).parse(fields);
        return { apiKey: new SecretString(decoded.api_key), token: new SecretString(decoded.token) };
      },
    };
    return new TrelloClient(config, credentials, options);
  });
}

/** Read Dex Web's pollInterval, Go duration nanoseconds or Go duration text, as whole milliseconds. */
function pollIntervalMilliseconds(value: unknown): number {
  if (typeof value === 'number' && Number.isSafeInteger(value)) return Math.floor(value / 1_000_000);
  if (typeof value !== 'string' || !value) throw new TypeError('Trello pollInterval must be a Go duration');
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, 'μs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  let rest = value, total = 0;
  while (rest) {
    const match = /^(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)/.exec(rest);
    if (!match) throw new TypeError('Trello pollInterval must be a Go duration');
    total += Number(match[1]) * units[match[2]!]!;
    rest = rest.slice(match[0].length);
  }
  return Math.floor(total);
}
