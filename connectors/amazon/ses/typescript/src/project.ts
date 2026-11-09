// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { AmazonSESClient } from './client.js';
import { newProjectConnection as generatedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

/** Open one named Amazon SES connection from Dex project configuration. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return generatedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'amazon-ses' && entry.connectionName === name);
    if (!selected) throw new TypeError('Amazon SES project connection is not configured');
    const fields = z.strictObject({ region: z.string().optional(), endpoint: z.string().optional(), timeout: z.union([z.string(), z.number().int()]).optional(), maxResponseBytes: z.number().int().optional() }).parse(selected.configuration);
    const timeoutMs = typeof fields.timeout === 'number' ? Math.ceil(fields.timeout / 1_000_000) : fields.timeout ? parseDurationMilliseconds(fields.timeout) : undefined;
    const config: Config = {
      ...(fields.region ? { region: fields.region } : {}),
      ...(fields.endpoint ? { endpoint: fields.endpoint } : {}),
      ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      ...(fields.maxResponseBytes !== undefined ? { maxResponseBytes: fields.maxResponseBytes } : {}),
    };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(_call, signal) {
        const privateFields = await loadedProject.resolveCredential('amazon-ses', name, signal);
        if (typeof privateFields.access_key_id !== 'string' || typeof privateFields.secret_access_key !== 'string') throw new TypeError('Amazon SES credentials are unavailable');
        return { accessKeyId: new SecretString(privateFields.access_key_id), secretAccessKey: new SecretString(privateFields.secret_access_key), sessionToken: new SecretString(typeof privateFields.session_token === 'string' ? privateFields.session_token : '') };
      },
    };
    return new AmazonSESClient(config, credentials);
  });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const part = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!part) throw new TypeError('Amazon SES timeout must be a positive Go duration');
    milliseconds += Number(part[1]) * (units[part[2] ?? ''] ?? NaN);
    remaining = remaining.slice(part[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError('Amazon SES timeout must resolve to positive whole milliseconds');
  return milliseconds;
}
