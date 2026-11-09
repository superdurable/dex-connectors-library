// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { newProjectCredentialProvider } from './generated/connection.js';
import { decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './models.js';
import { ConstantContactClient, type ConstantContactClientOptions } from './client.js';
import { newConnection } from './connection.js';

function encodeCredentials(credentials: Credentials): Record<string, string> {
  return {
    oauth_client_id: credentials.oauthClientId,
    oauth_client_secret: credentials.oauthClientSecret.reveal(),
    access_token: credentials.accessToken.reveal(),
    refresh_token: credentials.refreshToken.reveal(),
  };
}

/** Open one renewable Constant Contact connection from Dex project configuration. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: ConstantContactClientOptions = {}) {
  if (!project) throw new TypeError('Constant Contact project is required');
  const selected = project.configuration.connections.find(entry => entry.connectorId === 'constantcontact' && entry.connectionName === connectionName);
  if (!selected || selected.provider !== 'constantcontact') throw new TypeError('Constant Contact project connection is not configured');
  const fields = z.strictObject({
    baseUrl: z.string().optional(),
    timeout: z.union([z.string(), z.number().int()]).optional(),
    maxResponseBytes: z.number().int().optional(),
  }).parse(selected.configuration);
  const timeoutMs = typeof fields.timeout === 'number'
    ? Math.ceil(fields.timeout / 1_000_000)
    : fields.timeout ? parseDurationMilliseconds(fields.timeout) : undefined;
  const config: Config = {
    ...(fields.baseUrl !== undefined ? { baseUrl: fields.baseUrl } : {}),
    ...(timeoutMs !== undefined ? { timeoutMs } : {}),
    ...(fields.maxResponseBytes !== undefined ? { maxResponseBytes: fields.maxResponseBytes } : {}),
  };
  const provider = newProjectCredentialProvider(project, connectionName, decodeCredentials, encodeCredentials, decodeSharedCredentials);
  return newConnection(new ConstantContactClient(config, provider, options), { provider: 'constantcontact', name: connectionName });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const part = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!part) throw new TypeError('Constant Contact timeout must be a positive Go duration');
    milliseconds += Number(part[1]) * (units[part[2] ?? ''] ?? NaN);
    remaining = remaining.slice(part[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0)
    throw new TypeError('Constant Contact timeout must resolve to positive whole milliseconds');
  return milliseconds;
}
