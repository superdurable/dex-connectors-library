// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { XeroClient, type XeroClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return {
    auth_method: credentials.authMethodId ?? 'custom-connection',
    client_id: credentials.clientId,
    client_secret: credentials.clientSecret?.reveal() ?? '',
    access_token: credentials.accessToken?.reveal() ?? '',
    refresh_token: credentials.refreshToken?.reveal() ?? '',
  };
}

/** Open a named Xero connection with generated private and shared OAuth credential decoding. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: XeroClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'xero' && entry.connectionName === name);
    if (!selected) throw new TypeError('Xero project connection is not configured');
    const parsed = ConfigSchema.parse(selected.configuration);
    const config = { ...(parsed.maxResponseBytes !== undefined ? { maxResponseBytes: parsed.maxResponseBytes } : {}), ...(parsed.organisation !== undefined ? { organisation: parsed.organisation } : {}) };
    return new XeroClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}
