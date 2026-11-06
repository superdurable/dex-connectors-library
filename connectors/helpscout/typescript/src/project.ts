// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection, newProjectCredentialProvider } from './generated/connection.js';
import { HelpScoutClient, type HelpScoutClientOptions } from './client.js';

function encodeCredentials(credentials: Credentials): Record<string, unknown> {
  return {
    app_id: credentials.appId,
    app_secret: credentials.appSecret.reveal(),
    access_token: credentials.accessToken?.reveal() ?? '',
    webhook_secret: credentials.webhookSecret?.reveal() ?? '',
  };
}

/** Open a Help Scout project connection with generated renewable credential decoding. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: HelpScoutClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'helpscout' && entry.connectionName === name);
    if (!selected) throw new TypeError('Help Scout project connection is not configured');
    const settings = ConfigSchema.parse(selected.configuration);
    const config = {
      ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}),
      ...(settings.webhookMaxBodyBytes !== undefined ? { webhookMaxBodyBytes: settings.webhookMaxBodyBytes } : {}),
    };
    return new HelpScoutClient(config, newProjectCredentialProvider(loadedProject, name, decodeCredentials, encodeCredentials, decodeSharedCredentials), options);
  });
}
