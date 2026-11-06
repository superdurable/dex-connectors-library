// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { CalendlyClient, type CalendlyClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return {
    auth_method: credentials.authMethodId ?? 'personal-access-token',
    access_token: credentials.accessToken.reveal(),
    oauth_client_id: credentials.oauthClientId ?? '',
    oauth_client_secret: credentials.oauthClientSecret?.reveal() ?? '',
    refresh_token: credentials.refreshToken?.reveal() ?? '',
    webhook_signing_key: credentials.webhookSigningKey?.reveal() ?? '',
  };
}

/** Open a named Calendly connection with generated private and shared OAuth wiring. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: CalendlyClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'calendly' && entry.connectionName === name);
    if (!selected) throw new TypeError('Calendly project connection is not configured');
    const raw = selected.configuration as Record<string, unknown>;
    const parsed = ConfigSchema.parse({
      ...raw,
      ...(raw.webhookSignatureTolerance !== undefined ? { webhookSignatureToleranceMs: durationMilliseconds(raw.webhookSignatureTolerance) } : {}),
    });
    const config: Config = {
      ...(parsed.maxResponseBytes !== undefined ? { maxResponseBytes: parsed.maxResponseBytes } : {}),
      ...(parsed.webhookMaxBodyBytes !== undefined ? { webhookMaxBodyBytes: parsed.webhookMaxBodyBytes } : {}),
      ...(parsed.webhookSignatureToleranceMs !== undefined ? { webhookSignatureToleranceMs: parsed.webhookSignatureToleranceMs } : {}),
    };
    return new CalendlyClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}

function durationMilliseconds(value: unknown): number {
  if (typeof value === 'number') return Math.ceil(value / 1_000_000);
  if (typeof value !== 'string') throw new TypeError('Calendly duration must use Go duration syntax');
  let rest = value;
  let total = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60000, h: 3600000 };
  while (rest) {
    const match = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(rest);
    if (!match) throw new TypeError('Calendly duration must use Go duration syntax');
    total += Number(match[1]) * units[match[2]!]!;
    rest = rest.slice(match[0].length);
  }
  if (!Number.isFinite(total) || total <= 0 || total > Number.MAX_SAFE_INTEGER) throw new TypeError('Calendly duration must be positive');
  return Math.ceil(total);
}
