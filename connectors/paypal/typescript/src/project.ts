// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, type Config, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import { PayPalClient } from './client.js';

/** Open one static PayPal connection declared in a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'paypal' && entry.connectionName === name);
    if (!selected) throw new TypeError('PayPal project connection is not configured');
    const config = ConfigSchema.parse(selected.configuration) as Config;
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('paypal', name, signal);
        if (typeof fields.client_id !== 'string' || typeof fields.client_secret !== 'string') throw new TypeError('PayPal credentials are unavailable');
        return { clientId: fields.client_id, clientSecret: new SecretString(fields.client_secret) };
      },
    };
    return new PayPalClient(config, credentials);
  });
}
