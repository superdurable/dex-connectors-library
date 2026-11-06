// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { OAuth2HTTPClient, type OAuth2HTTPClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return { access_token: credentials.accessToken.reveal(), oauth_client_id: credentials.oauthClientId,
    oauth_client_secret: credentials.oauthClientSecret.reveal(), refresh_token: credentials.refreshToken?.reveal() ?? '' };
}

/** Open a named OAuth 2.0 HTTP connection with generated credential refresh. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: OAuth2HTTPClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'oauth2-http' && entry.connectionName === name);
    if (!selected) throw new TypeError('OAuth 2.0 HTTP project connection is not configured');
    const raw = selected.configuration as Record<string, unknown>;
    const settings = ConfigSchema.parse({ ...raw,
      ...(raw.requestTimeout !== undefined ? { requestTimeoutMs: parseDurationMilliseconds(raw.requestTimeout) } : {}),
    });
    const config: Config = { authorizationEndpoint: settings.authorizationEndpoint, tokenEndpoint: settings.tokenEndpoint,
      scopes: settings.scopes, apiBaseUrl: settings.apiBaseUrl,
      ...(settings.tokenEndpointAuthMethod !== undefined ? { tokenEndpointAuthMethod: settings.tokenEndpointAuthMethod } : {}),
      ...(settings.maxBodyBytes !== undefined ? { maxBodyBytes: settings.maxBodyBytes } : {}),
      ...(settings.requestTimeoutMs !== undefined ? { requestTimeoutMs: settings.requestTimeoutMs } : {}) };
    return new OAuth2HTTPClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}

function parseDurationMilliseconds(value: unknown): number {
  if (typeof value === 'number') return Math.ceil(value / 1_000_000);
  if (typeof value !== 'string') throw new TypeError('requestTimeout must be a positive Go duration');
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1_000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('requestTimeout must be a positive Go duration');
    milliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError('requestTimeout must resolve to positive whole milliseconds');
  return milliseconds;
}
