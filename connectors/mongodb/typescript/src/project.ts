// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Call, CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConfigSchema, decodeCredentials, type Config, type Credentials } from './generated/models.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import { MongoDBClient } from './client.js';

/** Open one statically named MongoDB connection from Dex project configuration. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(
      entry => entry.connectorId === 'mongodb' && entry.connectionName === name,
    );
    if (!selected) throw new TypeError('MongoDB project connection is not configured');
    const configuration = ConfigSchema.parse(selected.configuration) as Config;
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        return decodeCredentials(await loadedProject.resolveCredential('mongodb', name, signal));
      },
    };
    return new MongoDBClient(configuration, credentials);
  });
}
