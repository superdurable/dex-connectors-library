// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { IFLYTEKClient } from './client.js';
import { newProjectConnection as generatedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

/** Open one named IFLYTEK connection from Dex project configuration. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return generatedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'iflytek' && entry.connectionName === name);
    if (!selected) throw new TypeError('IFLYTEK project connection is not configured');
    const fields = z.strictObject({ baseUrl: z.string().optional(), timeout: z.union([z.string(), z.number().int()]).optional(), maxResponseBytes: z.number().int().optional(), model: z.string().optional(), maxOutputTokens: z.number().int().optional() }).parse(selected.configuration);
    const timeoutMs = typeof fields.timeout === 'number' ? Math.ceil(fields.timeout / 1_000_000) : fields.timeout ? parseDurationMilliseconds(fields.timeout) : undefined;
    const config: Config = {
      ...(fields.baseUrl !== undefined ? { baseUrl: fields.baseUrl } : {}),
      ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      ...(fields.maxResponseBytes !== undefined ? { maxResponseBytes: fields.maxResponseBytes } : {}),
      ...(fields.model !== undefined ? { model: fields.model } : {}),
      ...(fields.maxOutputTokens !== undefined ? { maxOutputTokens: fields.maxOutputTokens } : {}),
    };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(_call, signal) {
        const privateFields = await loadedProject.resolveCredential('iflytek', name, signal);
        if (typeof privateFields.apiPassword !== 'string') throw new TypeError('Spark APIPassword is unavailable');
        return { apiPassword: new SecretString(privateFields.apiPassword) };
      },
    };
    return new IFLYTEKClient(config, credentials);
  });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const part = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!part) throw new TypeError('IFLYTEK timeout must be a positive Go duration');
    milliseconds += Number(part[1]) * (units[part[2] ?? ''] ?? NaN);
    remaining = remaining.slice(part[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError('IFLYTEK timeout must resolve to positive whole milliseconds');
  return milliseconds;
}
