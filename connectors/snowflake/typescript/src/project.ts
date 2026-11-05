// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Call, CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { SnowflakeClient } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import { ConfigSchema, decodeCredentials, type Config, type Credentials } from './generated/models.js';

/** Resolve a static Snowflake connection from a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'snowflake' && entry.connectionName === name);
    if (!selected) throw new TypeError('Snowflake project connection is not configured');
    const config = ConfigSchema.parse(selected.configuration) as Config;
    const credentials: CredentialProvider<Credentials> = {
      async resolve(_call: Call, signal?: AbortSignal): Promise<Credentials> {
        return decodeCredentials(await loadedProject.resolveCredential('snowflake', name, signal));
      },
    };
    return new SnowflakeClient(config, credentials);
  });
}
