// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, type CredentialProvider } from '@superdurable/connectors-sdk';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { DatadogClient, newProjectConnection, type Config, type Credentials } from '../../src/index.js';
import { ConnectionName, MetricsLookupFlow } from './flow.js';

const shutdown = new AbortController();
process.once('SIGINT', () => shutdown.abort());
process.once('SIGTERM', () => shutdown.abort());

try {
  const project = await loadFromEnvironment();
  const connection = newProjectConnection(project, ConnectionName, (loaded, name) => {
    const selected = loaded.configuration.connections.find(entry => entry.connectorId === 'datadog' && entry.connectionName === name);
    if (!selected) throw new TypeError('Datadog project connection is unavailable');
    const settings = selected.configuration as Record<string, unknown>;
    const config: Config = {
      ...(typeof settings.baseUrl === 'string' ? { baseUrl: settings.baseUrl } : {}),
      ...(typeof settings.timeout === 'string' ? { timeoutMs: parseDurationMilliseconds(settings.timeout) } : {}),
      ...(typeof settings.timeout === 'number' ? { timeoutMs: Math.ceil(settings.timeout / 1_000_000) } : {}),
      ...(typeof settings.maxResponseBytes === 'number' ? { maxResponseBytes: settings.maxResponseBytes } : {}),
    };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(_call, signal) {
        const fields = await loaded.resolveCredential('datadog', name, signal);
        if (typeof fields.api_key !== 'string' || typeof fields.application_key !== 'string') throw new TypeError('Datadog credentials are unavailable');
        return { apiKey: new SecretString(fields.api_key), applicationKey: new SecretString(fields.application_key) };
      },
    };
    return new DatadogClient(config, credentials);
  });
  const cache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-datadog-metrics-blobs'), maxBytes: 1 << 30 });
  const worker = new Worker(new Registry([new MetricsLookupFlow(connection)]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000, signal: shutdown.signal });
    if (!shutdown.signal.aborted) await new Promise<void>(resolve => shutdown.signal.addEventListener('abort', () => resolve(), { once: true }));
  } finally { await worker.close(); cache.close(); }
} catch (error) {
  console.error('Datadog metrics Worker stopped', error instanceof Error ? error.name : 'unknown error');
  process.exitCode = 1;
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Datadog timeout must be a positive Go duration');
    milliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError('Datadog timeout must resolve to positive whole milliseconds');
  return milliseconds;
}
