// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { HiverClient } from './client.js';
import { newProjectConnection as bindProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

/** Open one static Hiver connection from a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return bindProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'hiver' && entry.connectionName === name);
    if (!selected) throw new TypeError('Hiver project connection is not configured');
    const config = z.object({ maxResponseBytes: z.number().int().optional(), requestIntervalMilliseconds: z.number().int().optional() }).parse(selected.configuration) as Config;
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('hiver', name, signal);
        const decoded = z.object({ api_key: z.string().min(1) }).parse(fields);
        return { apiKey: new SecretString(decoded.api_key) };
      },
    };
    return new HiverClient(config, credentials);
  });
}
