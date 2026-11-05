// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection as generatedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { decodeCredentials, decodeSharedCredentials, type Config, type Credentials } from './generated/models.js';
import { ExcelClient } from './client.js';

/** Encode complete private OAuth replacement fields for Dex project storage. */
export function encodeCredentials(credentials: Credentials): Record<string, string> {
  return { auth_method: credentials.authMethodId ?? 'microsoft-oauth', oauth_client_id: credentials.oauthClientId,
    oauth_client_secret: credentials.oauthClientSecret.reveal(), access_token: credentials.accessToken.reveal(),
    refresh_token: credentials.refreshToken.reveal() };
}

/** Bind one statically named Microsoft Excel project connection. */
export function newExcelProjectConnection(project: LoadedProject, connectionName: string) {
  return generatedProjectConnection(project, connectionName, (loaded, name) => {
    const entry = loaded.configuration.connections.find(connection => connection.connectorId === 'microsoft-excel' && connection.connectionName === name);
    if (!entry) throw new TypeError('Microsoft Excel project connection is not configured');
    const fields = entry.configuration as Record<string, unknown>;
    const config: Config = {
      ...(fields.maxResponseBytes !== undefined ? { maxResponseBytes: Number(fields.maxResponseBytes) } : {}),
      ...(fields.maxCells !== undefined ? { maxCells: Number(fields.maxCells) } : {}),
    };
    return new ExcelClient(config, newProjectCredentialProvider(loaded, name, decodeCredentials, encodeCredentials, decodeSharedCredentials));
  });
}
