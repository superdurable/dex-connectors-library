// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { GoogleFormsClient, type GoogleFormsClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return {
    auth_method: credentials.authMethodId ?? 'google-oauth',
    oauth_client_id: credentials.oauthClientId ?? '',
    service_account_key: credentials.serviceAccountKey?.reveal() ?? '',
    delegated_user: credentials.delegatedUser ?? '',
    oauth_client_secret: credentials.oauthClientSecret?.reveal() ?? '',
    access_token: credentials.accessToken?.reveal() ?? '',
    refresh_token: credentials.refreshToken?.reveal() ?? '',
  };
}

/** Open a named Google Forms connection with generated private and shared credential decoders. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: GoogleFormsClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'google-forms' && entry.connectionName === name);
    if (!selected) throw new TypeError('Google Forms project connection is not configured');
    const raw = selected.configuration as Record<string, unknown>;
    const settings = ConfigSchema.parse(raw);
    const config: Config = {
      ...(settings.endpoint !== undefined ? { endpoint: settings.endpoint } : {}),
      ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}),
      ...(settings.responsePageSize !== undefined ? { responsePageSize: settings.responsePageSize } : {}),
    };
    return new GoogleFormsClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}
