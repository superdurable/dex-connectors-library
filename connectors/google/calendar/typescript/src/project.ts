// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { CalendarClient, type CalendarClientOptions } from './client.js';

const projectSettingsSchema = ConfigSchema.extend({ timeout: z.union([z.string(), z.number().int()]).optional() }).strict();

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return {
    auth_method: credentials.authMethodId || 'google-oauth',
    ...(credentials.oauthClientId !== undefined ? { oauth_client_id: credentials.oauthClientId } : {}),
    ...(credentials.oauthClientSecret !== undefined ? { oauth_client_secret: credentials.oauthClientSecret.reveal() } : {}),
    ...(credentials.refreshToken !== undefined ? { refresh_token: credentials.refreshToken.reveal() } : {}),
    ...(credentials.serviceAccountKey !== undefined ? { service_account_key: credentials.serviceAccountKey.reveal() } : {}),
    ...(credentials.delegatedUser !== undefined ? { delegated_user: credentials.delegatedUser } : {}),
    access_token: credentials.accessToken?.reveal() ?? '',
  };
}

/** Open a named Calendar connection with generated OAuth and shared credential decoders. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: CalendarClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'google-calendar' && entry.connectionName === name);
    if (!selected) throw new TypeError('Google Calendar project connection is not configured');
    const raw = selected.configuration as Record<string, unknown>;
    const settings = projectSettingsSchema.parse(raw);
    const { timeout: configuredTimeout, ...config } = settings;
    const timeout = configuredTimeout === undefined ? undefined : parseDurationMilliseconds(configuredTimeout);
    return new CalendarClient({ ...config, ...(timeout === undefined ? {} : { timeout }) }, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}

function parseDurationMilliseconds(value: unknown): number {
  if (typeof value === 'number') {
    const milliseconds = Math.ceil(value / 1_000_000);
    if (Number.isSafeInteger(milliseconds) && milliseconds > 0) return milliseconds;
  }
  if (typeof value !== 'string') throw new TypeError('Google Calendar timeout must be a positive Go duration');
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1_000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Google Calendar timeout must be a positive Go duration');
    milliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError('Google Calendar timeout must resolve to positive whole milliseconds');
  return milliseconds;
}
