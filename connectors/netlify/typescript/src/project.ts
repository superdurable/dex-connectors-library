// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { NetlifyClient } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import { ConfigSchema, type Config, type Credentials } from './generated/models.js';

/** Open one static Netlify connection declared in a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'netlify' && entry.connectionName === name);
    if (!selected) throw new TypeError('Netlify project connection is not configured');
    const config = ConfigSchema.parse(selected.configuration) as Config;
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('netlify', name, signal);
        const decoded = z.object({ access_token: z.string().min(1) }).parse(fields);
        return { accessToken: new SecretString(decoded.access_token) };
      },
    };
    return new NetlifyClient(config, credentials);
  });
}
