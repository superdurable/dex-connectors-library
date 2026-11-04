// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Call, CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { TypeformClient } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import { ConfigSchema, decodeCredentials, type Config, type Credentials } from './generated/models.js';

/** Bind a static Typeform connection from a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'typeform' && entry.connectionName === name);
    if (!selected) throw new TypeError('Typeform project connection is not configured');
    const config = ConfigSchema.parse(selected.configuration) as Config;
    const credentials: CredentialProvider<Credentials> = {
      async resolve(_call: Call, signal?: AbortSignal): Promise<Credentials> {
        const fields = await loadedProject.resolveCredential('typeform', name, signal);
        return decodeCredentials(z.record(z.string(), z.unknown()).parse(fields));
      },
    };
    return new TypeformClient(config, credentials);
  });
}
