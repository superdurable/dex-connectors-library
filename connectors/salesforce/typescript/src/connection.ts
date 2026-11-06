// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import {
  newConnection as bindConnection,
  newProjectConnection as bindProjectConnection,
  newProjectCredentialProvider,
  type Connection,
} from './generated/connection.js';
import {
  decodeCredentials, decodeSharedCredentials,
  type Config, type Credentials,
} from './models.js';
import { encodeCredentials } from './credential_codec.js';
import { SalesforceClient, type SalesforceClientOptions } from './client.js';

export type { Connection } from './generated/connection.js';

/** Bind a Salesforce client to one static Dex connection reference. */
export const newConnection = bindConnection;

/** Bind a named Dex Web connection through the generated credential provider. */
export function newProjectConnection(
  project: LoadedProject, connectionName: string, options: SalesforceClientOptions = {},
): Connection {
  return bindProjectConnection(project, connectionName, (loaded, name) => {
    const selected = loaded.configuration.connections.find(
      entry => entry.connectorId === 'salesforce' && entry.connectionName === name,
    );
    if (!selected || selected.provider !== 'salesforce') throw new TypeError('Salesforce project connection is missing');
    const config: Config = {
      ...(typeof selected.configuration.apiVersion === 'string' ? { apiVersion: selected.configuration.apiVersion } : {}),
      ...(typeof selected.configuration.maxResponseBytes === 'number' ? { maxResponseBytes: selected.configuration.maxResponseBytes } : {}),
      ...(typeof selected.configuration.queryBatchSize === 'number' ? { queryBatchSize: selected.configuration.queryBatchSize } : {}),
    };
    const provider = newProjectCredentialProvider<Credentials>(
      loaded, name, decodeCredentials, encodeCredentials, decodeSharedCredentials,
    );
    return new SalesforceClient(config, provider, options);
  });
}
