// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { LinearClient, type LinearClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return credentials.authMethodId === 'linear-oauth' ? {
    auth_method: 'linear-oauth', oauth_client_id: credentials.oauthClientId ?? '', oauth_client_secret: credentials.oauthClientSecret?.reveal() ?? '',
    access_token: credentials.accessToken?.reveal() ?? '', refresh_token: credentials.refreshToken?.reveal() ?? '',
    webhook_signing_secret: credentials.webhookSigningSecret?.reveal() ?? '',
  } : { auth_method: 'personal-api-key', api_key: credentials.apiKey?.reveal() ?? '', webhook_signing_secret: credentials.webhookSigningSecret?.reveal() ?? '' };
}

/** Open a named Linear connection with generated private and shared credential decoders. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: LinearClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'linear' && entry.connectionName === name);
    if (!selected) throw new TypeError('Linear project connection is not configured');
    const raw = selected.configuration as Record<string, unknown>;
    const config = ConfigSchema.parse({ ...raw,
      ...(raw.webhookSignatureTolerance !== undefined ? { webhookSignatureToleranceMs: parseDurationMilliseconds(raw.webhookSignatureTolerance) } : {}),
    });
    const settings = {
      ...(config.maxResponseBytes !== undefined ? { maxResponseBytes: config.maxResponseBytes } : {}),
      ...(config.webhookMaxBodyBytes !== undefined ? { webhookMaxBodyBytes: config.webhookMaxBodyBytes } : {}),
      ...(config.webhookSignatureToleranceMs !== undefined ? { webhookSignatureToleranceMs: config.webhookSignatureToleranceMs } : {}),
    };
    return new LinearClient(settings, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}

function parseDurationMilliseconds(value: unknown): number {
  if (typeof value === 'number') return Math.ceil(value / 1_000_000);
  if (typeof value !== 'string') throw new TypeError('Linear duration must be a Go duration');
  let remaining = value, milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1_000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Linear duration must be a Go duration');
    milliseconds += Number(component[1]) * units[component[2]!]!;
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds < 1) throw new TypeError('Linear duration must resolve to positive whole milliseconds');
  return milliseconds;
}
