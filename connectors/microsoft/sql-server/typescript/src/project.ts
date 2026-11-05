// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, type Config, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import { SQLServerClient } from './client.js';

/** Resolve one static SQL Server connection from Dex project configuration. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'microsoft-sql-server' && entry.connectionName === name);
    if (!selected) throw new TypeError('SQL Server project connection is not configured');
    const config = ConfigSchema.parse(selected.configuration);
    const credentials: CredentialProvider<Credentials> = {
      async resolve(_call: Call, signal?: AbortSignal): Promise<Credentials> {
        const fields = await loadedProject.resolveCredential('microsoft-sql-server', name, signal);
        if (typeof fields.password !== 'string' || !fields.password) throw new TypeError('SQL Server password is unavailable');
        return { password: new SecretString(fields.password) };
      },
    };
    return new SQLServerClient(config as Config, credentials);
  });
}
