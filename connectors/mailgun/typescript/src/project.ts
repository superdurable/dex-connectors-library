// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, type Config, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import { MailgunClient } from './client.js';

/** Open one named Mailgun connection in a loaded Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'mailgun' && entry.connectionName === name);
    if (!selected) throw new TypeError('Mailgun project connection is not configured');
    const config = ConfigSchema.parse(selected.configuration) as Config;
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('mailgun', name, signal);
        if (typeof fields.api_key !== 'string') throw new TypeError('Mailgun API key is unavailable');
        return { apiKey: new SecretString(fields.api_key) };
      },
    };
    return new MailgunClient(config, credentials);
  });
}
