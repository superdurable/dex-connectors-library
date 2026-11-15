// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, type Config, type Credentials } from './generated/models.js';
import { newProjectConnection as bindProjectConnection } from './generated/connection.js';
import { FreshdeskClient, type FreshdeskClientOptions } from './client.js';

/**
 * Open a configured static Freshdesk connection from a loaded Dex project. options, such as a ticketChanged
 * logger or a local apiBaseURL for verification, apply to the connection's client.
 */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: FreshdeskClientOptions = {}) {
  return bindProjectConnection(project, connectionName, (loaded, name) => {
    const selected = loaded.configuration.connections.find(entry => entry.connectorId === 'freshdesk' && entry.connectionName === name);
    if (!selected) throw new TypeError('Freshdesk project connection is not configured');
    const raw = selected.configuration as Record<string, unknown>;
    const pollIntervalMs = raw.pollInterval === undefined || raw.pollInterval === null ? undefined : durationMilliseconds(raw.pollInterval);
    const parsed = ConfigSchema.parse({ ...raw, ...(pollIntervalMs !== undefined ? { pollIntervalMs } : {}) });
    const config: Config = {
      domain: parsed.domain,
      ...(parsed.maxResponseBytes !== undefined ? { maxResponseBytes: parsed.maxResponseBytes } : {}),
      ...(parsed.pollIntervalMs !== undefined ? { pollIntervalMs: parsed.pollIntervalMs } : {}),
    };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loaded.resolveCredential('freshdesk', name, signal);
        const apiKey = fields.api_key;
        if (typeof apiKey !== 'string' || !apiKey) throw new TypeError('Freshdesk API key is unavailable');
        return { apiKey: new SecretString(apiKey) };
      },
    };
    return new FreshdeskClient(config, credentials, options);
  });
}

// The project stores pollInterval as a Go time.Duration: integer nanoseconds, as the Go and Python connectors
// decode it. Zero keeps the one-minute default, as Go does; any other type is rejected.
function durationMilliseconds(value: unknown): number | undefined {
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('Freshdesk configuration pollInterval has an invalid type');
  return value === 0 ? undefined : Math.ceil(value / 1_000_000);
}
