// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { newProjectConnection as generatedProjectConnection } from './generated/connection.js';
import { WebflowClient } from './client.js';
import type { Config, Credentials } from './generated/models.js';

/** Open one named Webflow connection from a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return generatedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'webflow' && entry.connectionName === name);
    if (!selected) throw new TypeError('Webflow project connection is not configured');
    const fields = z.strictObject({ baseUrl: z.string().optional(), timeout: z.union([z.string(), z.number().int()]).optional(), timeoutMs: z.number().int().optional(), maxResponseBytes: z.number().int().optional() }).parse(selected.configuration);
    const timeoutMs = fields.timeoutMs ?? (typeof fields.timeout === 'number' ? Math.ceil(fields.timeout / 1_000_000) : fields.timeout !== undefined ? parseDuration(fields.timeout) : undefined);
    const config: Config = {
      ...(fields.baseUrl !== undefined ? { baseUrl: fields.baseUrl } : {}),
      ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      ...(fields.maxResponseBytes !== undefined ? { maxResponseBytes: fields.maxResponseBytes } : {}),
    };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('webflow', name, signal);
        const decoded = z.object({ site_token: z.string().min(1) }).parse(fields);
        return { siteToken: new SecretString(decoded.site_token) };
      },
    };
    return new WebflowClient(config, credentials);
  });
}
function parseDuration(value: string): number {
  if (value === '0') return 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, 'μs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  let remaining = value;
  let milliseconds = 0;
  while (remaining) {
    const match = /^(\d+(?:\.\d+)?)(ns|us|µs|μs|ms|s|m|h)/.exec(remaining);
    if (!match) throw new TypeError('Webflow timeout must be a Go duration');
    milliseconds += Number(match[1]) * units[match[2]!]!;
    remaining = remaining.slice(match[0].length);
  }
  if (!Number.isFinite(milliseconds) || milliseconds < 0) throw new TypeError('Webflow timeout is invalid');
  return Math.ceil(milliseconds);
}
