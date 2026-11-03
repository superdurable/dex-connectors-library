// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { ConnectionRef } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { Client, type ClientOptions } from './client.js';
import { Connection, newConnection, newConnectionWithOperations, type Operations } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

export { Connection, newConnection, newConnectionWithOperations };
export type { Operations };

/** Open the static Asana connection from a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: ClientOptions = {}): Connection {
  if (!project || !connectionName.trim()) throw new TypeError('Asana project connection is required');
  const selected = project.configuration.connections.find(entry => entry.connectorId === 'asana' && entry.connectionName === connectionName);
  if (!selected || selected.provider !== 'asana') throw new TypeError('Asana project connection is not configured');
  const settings = z.object({ endpoint: z.string().optional(), maxResponseBytes: z.number().int().optional() }).parse(selected.configuration);
  const config: Config = { ...(settings.endpoint !== undefined ? { endpoint: settings.endpoint } : {}), ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}) };
  const credentials: CredentialProvider<Credentials> = {
    async resolve(_call: Call, signal?: AbortSignal): Promise<Credentials> {
      const fields = z.object({ access_token: z.string().min(1) }).parse(await project.resolveCredential('asana', connectionName, signal));
      return { accessToken: new SecretString(fields.access_token) };
    },
  };
  return newConnection(new Client(config, credentials, options), { provider: 'asana', name: connectionName } satisfies ConnectionRef);
}
