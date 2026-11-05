// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { DocuSignClient, type DocuSignClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return {
    auth_method: credentials.authMethodId ?? 'docusign-oauth',
    oauth_client_id: credentials.oauthClientId,
    oauth_client_secret: credentials.oauthClientSecret?.reveal() ?? '',
    access_token: credentials.accessToken.reveal(),
    refresh_token: credentials.refreshToken?.reveal() ?? '',
    connect_hmac_key: credentials.connectHmacKey?.reveal() ?? '',
  };
}

/** Open a named DocuSign connection with generated private and shared credential decoders. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: DocuSignClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'docusign' && entry.connectionName === name);
    if (!selected) throw new TypeError('DocuSign project connection is not configured');
    const parsed = ConfigSchema.parse(selected.configuration);
    const config = {
      ...(parsed.accountId !== undefined ? { accountId: parsed.accountId } : {}),
      ...(parsed.maxResponseBytes !== undefined ? { maxResponseBytes: parsed.maxResponseBytes } : {}),
      ...(parsed.maxDocumentBytes !== undefined ? { maxDocumentBytes: parsed.maxDocumentBytes } : {}),
      ...(parsed.connectMaxBodyBytes !== undefined ? { connectMaxBodyBytes: parsed.connectMaxBodyBytes } : {}),
    };
    return new DocuSignClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}
