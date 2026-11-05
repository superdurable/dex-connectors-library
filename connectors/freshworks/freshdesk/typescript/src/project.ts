// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, type Credentials } from './generated/models.js';
import { newProjectConnection as bindProjectConnection } from './generated/connection.js';
import { FreshdeskClient } from './client.js';

/** Open a configured static Freshdesk connection from a loaded Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return bindProjectConnection(project, connectionName, (loaded, name) => {
    const selected = loaded.configuration.connections.find(entry => entry.connectorId === 'freshdesk' && entry.connectionName === name);
    if (!selected) throw new TypeError('Freshdesk project connection is not configured');
    const config = ConfigSchema.parse(selected.configuration);
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loaded.resolveCredential('freshdesk', name, signal);
        const apiKey = fields.api_key;
        if (typeof apiKey !== 'string' || !apiKey) throw new TypeError('Freshdesk API key is unavailable');
        return { apiKey: new SecretString(apiKey) };
      },
    };
    return new FreshdeskClient({ domain: config.domain, ...(config.maxResponseBytes !== undefined ? { maxResponseBytes: config.maxResponseBytes } : {}) }, credentials);
  });
}
