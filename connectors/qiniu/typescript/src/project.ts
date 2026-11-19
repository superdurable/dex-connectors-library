// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Call, CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, type Config, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import { QiniuClient, type QiniuClientOptions } from './client.js';

/** Open a named Qiniu connection using generated private and shared credential decoders. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: QiniuClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'qiniu' && entry.connectionName === name);
    if (!selected) throw new TypeError('Qiniu project connection is not configured');
    const raw = selected.configuration as Record<string, unknown>;
    const settings = ConfigSchema.parse({ ...raw,
      ...(raw.timeout !== undefined ? { timeoutMs: typeof raw.timeout === 'number' ? Math.ceil(raw.timeout / 1_000_000) : parseDurationMilliseconds(raw.timeout) } : {}),
    });
    const config: Config = {
      ...(settings.baseUrl !== undefined ? { baseUrl: settings.baseUrl } : {}),
      ...(settings.timeoutMs !== undefined ? { timeoutMs: settings.timeoutMs } : {}),
      ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}),
    };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        return decodeCredentials(await loadedProject.resolveCredential('qiniu', name, signal));
      },
    };
    return new QiniuClient(config, credentials, options);
  });
}

function parseDurationMilliseconds(value: unknown): number {
  if (typeof value !== 'string') throw new TypeError('Qiniu timeout must be a positive Go duration');
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1_000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Qiniu timeout must be a positive Go duration');
    milliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError('Qiniu timeout must resolve to positive whole milliseconds');
  return milliseconds;
}
