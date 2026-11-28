// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectCredentialProvider } from './generated/connection.js';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './models.js';
import { WorkdayClient, type WorkdayClientOptions } from './client.js';
import { newConnection } from './connection.js';

function encodeCredentials(credentials: Credentials): Record<string, string> {
  return {
    oauth_client_id: credentials.oauthClientId,
    oauth_client_secret: credentials.oauthClientSecret.reveal(),
    access_token: credentials.accessToken.reveal(),
    refresh_token: credentials.refreshToken.reveal(),
  };
}

/** Open a statically named renewable Workday connection from Dex project configuration. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: WorkdayClientOptions = {}) {
  if (!project) throw new TypeError('Workday project is required');
  const selected = project.configuration.connections.find(entry => entry.connectorId === 'workday' && entry.connectionName === connectionName);
  if (!selected || selected.provider !== 'workday') throw new TypeError('Workday project connection is not configured');
  const fields = selected.configuration;
  let config: Config = ConfigSchema.parse(fields);
  if (fields.timeout !== undefined && fields.timeout !== '') {
    if (typeof fields.timeout !== 'number' || !Number.isSafeInteger(fields.timeout) || fields.timeout <= 0)
      throw new TypeError('Workday timeout must be a positive nanosecond duration');
    config = { ...config, timeoutMs: Math.ceil(fields.timeout / 1_000_000) };
  }
  const provider = newProjectCredentialProvider(project, connectionName, decodeCredentials, encodeCredentials, decodeSharedCredentials);
  return newConnection(new WorkdayClient(config, provider, options), { provider: 'workday', name: connectionName });
}
