// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { EntraClient } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './generated/models.js';

/** Encode private credentials for an atomic refresh replacement. */
export function encodeCredentials(credentials: Credentials): Record<string, string> {
  const fields: Record<string, string> = { auth_method: credentials.authMethodId ?? 'entra-app-only', client_id: credentials.clientId };
  for (const [name, value] of [['tenant_id', credentials.tenantId], ['client_secret', credentials.clientSecret?.reveal()], ['access_token', credentials.accessToken?.reveal()], ['refresh_token', credentials.refreshToken?.reveal()]] as const) if (value) fields[name] = value;
  return fields;
}

/** Open a statically named Microsoft Entra ID connection from a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const entry = loadedProject.configuration.connections.find(connection => connection.connectorId === 'microsoft-entra-id' && connection.connectionName === name);
    if (!entry) throw new TypeError('Microsoft Entra ID project connection is not configured');
    const settings = entry.configuration as Record<string, unknown>;
    for (const name of ['maxResponseBytes', 'listUsersPageSize']) if (settings[name] !== undefined && (typeof settings[name] !== 'number' || !Number.isInteger(settings[name]))) throw new TypeError(`Microsoft Entra ID ${name} must be an integer`);
    if (settings.creationKeyAttribute !== undefined && typeof settings.creationKeyAttribute !== 'string') throw new TypeError('Microsoft Entra ID creationKeyAttribute must be text');
    const config: Config = { ...(typeof settings.maxResponseBytes === 'number' ? { maxResponseBytes: settings.maxResponseBytes } : {}), ...(typeof settings.listUsersPageSize === 'number' ? { listUsersPageSize: settings.listUsersPageSize } : {}), ...(typeof settings.creationKeyAttribute === 'string' ? { creationKeyAttribute: settings.creationKeyAttribute } : {}) };
    const provider = newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials);
    return new EntraClient(config, provider);
  });
}
